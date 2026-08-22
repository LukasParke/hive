package proxy

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"github.com/moby/moby/api/types/swarm"
)

// --- fakes ---

// fakeRows replays canned rows for a single Scan signature.
type fakeRows struct {
	rows [][]any
	i    int
	err  error // returned by Scan for every row when non-nil
}

func (r *fakeRows) Next() bool {
	if r.i >= len(r.rows) {
		return false
	}
	r.i++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	row := r.rows[r.i-1]
	for j := range dest {
		if j < len(row) {
			reflect.ValueOf(dest[j]).Elem().Set(reflect.ValueOf(row[j]))
		}
	}
	return nil
}

func (r *fakeRows) Err() error                                   { return nil }
func (r *fakeRows) Close()                                       {}
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }

// fakeQuerier serves one canned result per Query call in order; a nil
// result entry (or exhausted list) yields errQueryBoom.
type fakeQuerier struct {
	results []*fakeRows
	calls   []string
}

var errQueryBoom = errors.New("query boom")

func (f *fakeQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.calls = append(f.calls, sql)
	if len(f.results) == 0 {
		return nil, errQueryBoom
	}
	r := f.results[0]
	f.results = f.results[1:]
	if r == nil {
		return nil, errQueryBoom
	}
	return r, nil
}

type errStore struct{ err error }

func (e *errStore) ListServices(context.Context) ([]swarm.Service, error) {
	return nil, e.err
}

func (e *errStore) UpdateService(context.Context, string, uint64, swarm.ServiceSpec) error {
	return e.err
}

// labeledStore returns one service labeled with the target application and
// records the final spec pushed by UpdateService.
type labeledStore struct {
	labels map[string]string
	spec   swarm.ServiceSpec
	calls  int
}

func (l *labeledStore) ListServices(context.Context) ([]swarm.Service, error) {
	labels := l.labels
	if labels == nil {
		labels = map[string]string{}
	}
	labels["hive.app.id"] = "app-1"
	return []swarm.Service{{
		ID:   "svc-1",
		Meta: swarm.Meta{Version: swarm.Version{Index: 7}},
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Labels: labels}},
	}}, nil
}

func (l *labeledStore) UpdateService(_ context.Context, _ string, _ uint64, spec swarm.ServiceSpec) error {
	l.spec = spec
	l.calls++
	return nil
}

// hostRows builds a domains-query result.
func hostRows(hosts ...string) *fakeRows {
	rows := &fakeRows{}
	for _, h := range hosts {
		rows.rows = append(rows.rows, []any{h})
	}
	return rows
}

// ruleRows builds a security_rules-query result.
func ruleRows(rows ...[]any) *fakeRows { return &fakeRows{rows: rows} }

const routerKey = "traefik.http.routers.app-app-example-com.middlewares"

func apply(t *testing.T, q *fakeQuerier, store ServiceStore) error {
	t.Helper()
	return applySecurityRules(context.Background(), q, store, "app-1")
}

// --- happy path ---

func TestApplySecurityRulesHappyPath(t *testing.T) {
	q := &fakeQuerier{results: []*fakeRows{
		hostRows("app.example.com"),
		ruleRows(
			[]any{"11111111", "ip_allowlist", []byte(`{"sourceRange":["10.0.0.0/8","192.168.0.0/16"]}`), int32(10)},
			[]any{"22222222", "header_security", []byte(`{"stsSeconds":31536000,"forceSTSHeader":true,"contentSecurityPolicy":"default-src 'self'","frameDeny":"true"}`), int32(5)},
			[]any{"33333333", "rate_limit", []byte(`{"average":100,"burst":200}`), int32(1)},
			// Unsupported legacy type emits nothing but must not fail.
			[]any{"44444444", "country_block", []byte(`{"countries":["DE"]}`), int32(0)},
			// Empty config emits nothing either.
			[]any{"55555555", "ip_allowlist", []byte(`{"sourceRange":[]}`), int32(0)},
		),
	}}
	store := &labeledStore{labels: map[string]string{}}
	if err := apply(t, q, store); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("expected exactly one service update, got %d", store.calls)
	}
	labels := store.spec.Labels
	want := []struct{ key, val string }{
		{"traefik.http.middlewares.hive-sec-11111111.ipallowlist.sourcerange", "10.0.0.0/8,192.168.0.0/16"},
		{"traefik.http.middlewares.hive-sec-22222222.headers.stsSeconds", "31536000"},
		{"traefik.http.middlewares.hive-sec-22222222.headers.forcestsheader", "true"},
		{"traefik.http.middlewares.hive-sec-22222222.headers.contentsecuritypolicy", "default-src 'self'"},
		{"traefik.http.middlewares.hive-sec-22222222.headers.framedeny", "true"},
		{"traefik.http.middlewares.hive-sec-33333333.ratelimit.average", "100"},
		{"traefik.http.middlewares.hive-sec-33333333.ratelimit.burst", "200"},
		{routerKey, "hive-sec-11111111,hive-sec-22222222,hive-sec-33333333"},
	}
	for _, w := range want {
		if labels[w.key] != w.val {
			t.Errorf("label %q = %q, want %q", w.key, labels[w.key], w.val)
		}
	}
}

func TestApplySecurityRulesPreservesForeignMiddlewares(t *testing.T) {
	q := &fakeQuerier{results: []*fakeRows{
		hostRows("app.example.com"),
		ruleRows([]any{"aaaaaaaa", "ip_allowlist", []byte(`{"sourceRange":["10.0.0.0/8"]}`), int32(1)}),
	}}
	store := &labeledStore{labels: map[string]string{
		routerKey: " other-mw , hive-sec-aaaaaaaa , app-app-example-com-strip ",
	}}
	if err := apply(t, q, store); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got := store.spec.Labels[routerKey]
	// Foreign entries are kept first; this run's security middlewares are
	// appended after them. The previous hive-sec entry is re-emitted so it
	// stays in the security set and is not duplicated.
	want := "other-mw,app-app-example-com-strip,hive-sec-aaaaaaaa"
	if got != want {
		t.Fatalf("merged middlewares = %q, want %q", got, want)
	}
}

func TestApplySecurityRulesKeepsPriorLabelWhenNothingEmitted(t *testing.T) {
	// With no enabled rules nothing is in this run's security set, so any
	// prior middleware value survives untouched — including stale hive-sec
	// entries from rules that have since been disabled or deleted.
	q := &fakeQuerier{results: []*fakeRows{
		hostRows("app.example.com"),
		ruleRows(),
	}}
	store := &labeledStore{labels: map[string]string{
		routerKey: "hive-sec-deadbeef,other-mw",
	}}
	if err := apply(t, q, store); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := store.spec.Labels[routerKey]; got != "hive-sec-deadbeef,other-mw" {
		t.Fatalf("middlewares = %q, want prior list preserved verbatim", got)
	}
}

func TestApplySecurityRulesMultipleHostsShareMiddlewares(t *testing.T) {
	q := &fakeQuerier{results: []*fakeRows{
		hostRows("app.example.com", "", "*.example.com"), // empty host is skipped
		ruleRows([]any{"bbbbbbbb", "rate_limit", []byte(`{"average":5}`), int32(1)}),
	}}
	store := &labeledStore{labels: nil} // nil Labels map must be initialized
	if err := apply(t, q, store); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, router := range []string{"app-app-example-com", "app-example-com"} {
		key := "traefik.http.routers." + router + ".middlewares"
		if store.spec.Labels[key] != "hive-sec-bbbbbbbb" {
			t.Errorf("router %s middlewares = %q", router, store.spec.Labels[key])
		}
	}
}

// --- edge branches ---

func TestApplySecurityRulesExportedWrapper(t *testing.T) {
	// The production entry point takes the concrete pool/client types; with
	// an unreachable swarm socket the very first call fails fast, which is
	// enough to exercise the delegation seam end-to-end.
	cli, err := newSwarmClientForTest()
	if err != nil {
		t.Fatalf("construct swarm client: %v", err)
	}
	if err := ApplySecurityRulesForApplication(context.Background(), nil, cli, "app-1"); err == nil {
		t.Fatal("expected list failure against unreachable socket")
	}
}

func TestComposeMiddlewaresDropsLabelWhenListEmpties(t *testing.T) {
	router := "app-example-com"
	labels := map[string]string{
		"traefik.http.routers." + router + ".middlewares":                    router + "-strip",
		"traefik.http.middlewares." + router + "-strip.stripprefix.prefixes": "/old",
	}
	composeMiddlewares(labels, router, Route{Host: "x.example.com"})
	if _, ok := labels["traefik.http.routers."+router+".middlewares"]; ok {
		t.Fatal("middlewares label must be dropped once its only entry is gone")
	}
}

func newSwarmClientForTest() (*swarmclient.Client, error) {
	return swarmclient.New("unix:///nonexistent-hive-test.sock")
}

func TestApplySecurityRulesNoMatchingService(t *testing.T) {
	q := &fakeQuerier{}
	err := applySecurityRules(context.Background(), q, &labeledStore{}, "absent")
	if err != nil {
		t.Fatalf("expected nil for undeployed app, got %v", err)
	}
	if len(q.calls) != 0 {
		t.Fatal("must not touch the database without a deployed service")
	}
}

func TestApplySecurityRulesListError(t *testing.T) {
	err := applySecurityRules(context.Background(), &fakeQuerier{}, &errStore{err: errors.New("swarm down")}, "app-1")
	if err == nil || err.Error() != "swarm down" {
		t.Fatalf("expected list error passthrough, got %v", err)
	}
}

func TestApplySecurityRulesQueryErrors(t *testing.T) {
	domainsFail := &fakeQuerier{results: []*fakeRows{nil}} // first Query errors
	if err := apply(t, domainsFail, &labeledStore{}); !errors.Is(err, errQueryBoom) {
		t.Fatalf("expected domains query error, got %v", err)
	}
	rulesFail := &fakeQuerier{results: []*fakeRows{hostRows("app.example.com"), nil}}
	if err := apply(t, rulesFail, &labeledStore{}); !errors.Is(err, errQueryBoom) {
		t.Fatalf("expected rules query error, got %v", err)
	}
}

func TestApplySecurityRulesScanErrorSkipsRow(t *testing.T) {
	bad := &fakeRows{rows: [][]any{{"66666666", "ip_allowlist"}}, err: fmt.Errorf("scan mismatch")}
	q := &fakeQuerier{results: []*fakeRows{
		hostRows("app.example.com"),
		bad, // every row fails to scan; must be skipped, not fatal
	}}
	store := &labeledStore{}
	if err := apply(t, q, store); err != nil {
		t.Fatalf("scan errors must be skipped, got %v", err)
	}
	if _, ok := store.spec.Labels[routerKey]; ok {
		t.Fatal("no middleware expected from unscannable rule")
	}
}

func TestApplySecurityRulesUpdateError(t *testing.T) {
	q := &fakeQuerier{results: []*fakeRows{hostRows("app.example.com"), ruleRows()}}
	err := applySecurityRules(context.Background(), q, &errStore{err: errors.New("update refused")}, "app-1")
	if err == nil || err.Error() != "update refused" {
		t.Fatalf("expected update error passthrough, got %v", err)
	}
}

// --- extract helpers table tests ---

func TestExtractStringSlice(t *testing.T) {
	cases := []struct {
		json, key string
		want      []string
	}{
		{`{"sourceRange":["a","b"]}`, "sourceRange", []string{"a", "b"}},
		{`{"sourceRange":[]}`, "sourceRange", nil},
		{`{"sourceRange":["a","","b"]}`, "sourceRange", []string{"a", "b"}},
		{`{"other":["a"]}`, "sourceRange", nil},     // missing key
		{`{"sourceRange":["a"`, "sourceRange", nil}, // unterminated array
		{`{"sourceRange":[`, "sourceRange", nil},    // no closing bracket at all
	}
	for _, tc := range cases {
		if got := extractStringSlice(tc.json, tc.key); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("extractStringSlice(%q,%q)=%v want %v", tc.json, tc.key, got, tc.want)
		}
	}
}

func TestExtractString(t *testing.T) {
	cases := []struct {
		json, key, want string
	}{
		{`{"csp":"default-src"}`, "csp", "default-src"},
		{`{"csp":"a\"b"}`, "csp", "a\\"}, // stops at first quote (naive parser)
		{`{"other":"x"}`, "csp", ""},
		{`{"csp":"unterminated}`, "csp", ""},
	}
	for _, tc := range cases {
		if got := extractString(tc.json, tc.key); got != tc.want {
			t.Errorf("extractString(%q,%q)=%q want %q", tc.json, tc.key, got, tc.want)
		}
	}
}

func TestExtractInt(t *testing.T) {
	cases := []struct {
		json, key string
		want      int
	}{
		{`{"n":42}`, "n", 42},
		{`{"n":-7}`, "n", -7},
		{`{"n":0}`, "n", 0},
		{`{"n":"abc"}`, "n", 0},    // non-numeric parses as 0
		{`{"other":9}`, "n", 0},    // missing key
		{`{"n":}`, "n", 0},         // empty value slot
		{`{"n":123abc}`, "n", 123}, // trailing junk ignored
	}
	for _, tc := range cases {
		if got := extractInt(tc.json, tc.key); got != tc.want {
			t.Errorf("extractInt(%q,%q)=%d want %d", tc.json, tc.key, got, tc.want)
		}
	}
}

func TestExtractBool(t *testing.T) {
	cases := []struct {
		json, key string
		want      bool
	}{
		{`{"b":true}`, "b", true},
		{`{"b":false}`, "b", false},
		{`{"b":truely}`, "b", true}, // prefix match is intentional naive parsing
		{`{"other":true}`, "b", false},
	}
	for _, tc := range cases {
		if got := extractBool(tc.json, tc.key); got != tc.want {
			t.Errorf("extractBool(%q,%q)=%v want %v", tc.json, tc.key, got, tc.want)
		}
	}
}
