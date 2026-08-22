package tunnels

import (
	"context"
	"errors"
	"testing"

	"github.com/luke/hive/control-plane/internal/testdb"
)

// newRepo wires a SQLRepo over the shared test database with a clean
// tunnels table.
func newRepo(t *testing.T) *SQLRepo {
	t.Helper()
	pool := testdb.Get(t)
	testdb.Truncate(t, "tunnels")
	return NewSQLRepo(pool)
}

func sampleRow(name string) *Row {
	return &Row{
		Name:                 name,
		CfTunnelID:           "cf-" + name,
		AccountID:            "acc-1",
		ZoneID:               "zone-1",
		CredentialSecretName: "tunnel:cf-" + name,
		Ingress:              []IngressRule{{Hostname: name + ".example.com", Service: "http://traefik:80"}},
		DNSRecords:           map[string]string{name + ".example.com": "rec-1"},
		Status:               StatusCreating,
	}
}

func TestSQLRepoCreateAndGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	row := sampleRow("edge-1")
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if row.ID == "" {
		t.Fatal("expected generated id to be written back")
	}

	got, err := repo.Get(ctx, row.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "edge-1" || got.CfTunnelID != "cf-edge-1" || got.AccountID != "acc-1" {
		t.Fatalf("unexpected row: %+v", got)
	}
	if got.ZoneID != "zone-1" {
		t.Fatalf("zone id = %q", got.ZoneID)
	}
	if len(got.Ingress) != 1 || got.Ingress[0].Hostname != "edge-1.example.com" {
		t.Fatalf("ingress = %+v", got.Ingress)
	}
	if got.DNSRecords["edge-1.example.com"] != "rec-1" {
		t.Fatalf("dns records = %v", got.DNSRecords)
	}
	if got.Status != StatusCreating {
		t.Fatalf("status = %q", got.Status)
	}
}

func TestSQLRepoGetByNameAndListOrdering(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	if _, err := repo.GetByName(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing name, got %v", err)
	}
	for _, n := range []string{"a-tunnel", "b-tunnel"} {
		if err := repo.Create(ctx, sampleRow(n)); err != nil {
			t.Fatalf("Create %s: %v", n, err)
		}
	}
	got, err := repo.GetByName(ctx, "b-tunnel")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.CfTunnelID != "cf-b-tunnel" {
		t.Fatalf("unexpected row %+v", got)
	}
	rows, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("List returned %d rows, want 2", len(rows))
	}
	if rows[0].Name != "b-tunnel" || rows[1].Name != "a-tunnel" {
		t.Fatalf("List must return newest first, got [%s %s]", rows[0].Name, rows[1].Name)
	}
}

func TestSQLRepoUniqueViolationsMapToConflict(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	if err := repo.Create(ctx, sampleRow("dup")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	dup := sampleRow("dup")
	if err := repo.Create(ctx, dup); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name: expected ErrConflict, got %v", err)
	}
	dup2 := sampleRow("dup2")
	dup2.CfTunnelID = "cf-dup"
	if err := repo.Create(ctx, dup2); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate tunnel id: expected ErrConflict, got %v", err)
	}
}

func TestSQLRepoGetInvalidUUID(t *testing.T) {
	repo := newRepo(t)
	if _, err := repo.Get(context.Background(), "not-a-uuid"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if err := repo.UpdateIngress(context.Background(), "bad-uuid", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput from UpdateIngress, got %v", err)
	}
	if err := repo.UpdateDNSRecords(context.Background(), "bad-uuid", nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput from UpdateDNSRecords, got %v", err)
	}
	if err := repo.SetStatus(context.Background(), "bad-uuid", StatusDeployed, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput from SetStatus, got %v", err)
	}
	if err := repo.Delete(context.Background(), "bad-uuid"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput from Delete, got %v", err)
	}
}

func TestSQLRepoGetMissingIDIsNotFound(t *testing.T) {
	repo := newRepo(t)
	_, err := repo.Get(context.Background(), "00000000-0000-4000-8000-000000000001")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSQLRepoUpdateIngressAndDNSRecords(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	row := sampleRow("mutate-me")
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rules := []IngressRule{
		{Hostname: "new.example.com", Path: "/x", Service: "https://upstream:443"},
	}
	if err := repo.UpdateIngress(ctx, row.ID, rules); err != nil {
		t.Fatalf("UpdateIngress: %v", err)
	}
	if err := repo.UpdateDNSRecords(ctx, row.ID, map[string]string{"new.example.com": "rec-9"}); err != nil {
		t.Fatalf("UpdateDNSRecords: %v", err)
	}

	got, err := repo.Get(ctx, row.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Ingress) != 1 || got.Ingress[0].Path != "/x" {
		t.Fatalf("persisted ingress = %+v", got.Ingress)
	}
	if got.DNSRecords["new.example.com"] != "rec-9" {
		t.Fatalf("persisted dns = %v", got.DNSRecords)
	}
}

func TestSQLRepoSetStatusPersistsError(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	row := sampleRow("status-row")
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.SetStatus(ctx, row.ID, StatusError, "boom"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	got, err := repo.Get(ctx, row.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != StatusError || got.ErrorMessage != "boom" {
		t.Fatalf("status=%q error=%q", got.Status, got.ErrorMessage)
	}
}

func TestSQLRepoDeleteAndForgetSecrets(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	repo := NewSQLRepo(pool)

	row := sampleRow("doomed")
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Seed the encrypted secrets the delete path purges.
	names := []string{row.CredentialSecretName, apiTokenSecretKey(row.CfTunnelID)}
	for _, name := range names {
		if _, err := pool.Exec(ctx,
			`insert into secrets_store(name, type, encrypted_value) values ($1, $2, 'x')`, name, SecretType); err != nil {
			t.Fatalf("seed secret %s: %v", name, err)
		}
	}

	if err := repo.ForgetSecrets(ctx, names); err != nil {
		t.Fatalf("ForgetSecrets: %v", err)
	}
	if n := testdb.QueryCount(t, `select count(*) from secrets_store where name = any($1)`, names); n != 0 {
		t.Fatalf("%d secrets survived ForgetSecrets", n)
	}

	if err := repo.Delete(ctx, row.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, row.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after Delete, got %v", err)
	}
}

func TestSQLRepoHelpers(t *testing.T) {
	// marshal/unmarshal round trips and defensive parsing of nullable text.
	txt := pgtypeText("z")
	if !txt.Valid || txt.String != "z" {
		t.Fatalf("pgtypeText broken: %+v", txt)
	}
	if textValue(txt) != "z" || textValue(pgtypeText("")) != "" {
		t.Fatal("textValue broken")
	}
	rules, err := unmarshalRules(nil)
	if err != nil || len(rules) != 0 {
		t.Fatalf("unmarshalRules(nil) = %v, %v", rules, err)
	}
	dns, err := unmarshalDNS(nil)
	if err != nil || len(dns) != 0 {
		t.Fatalf("unmarshalDNS(nil) = %v, %v", dns, err)
	}
	if _, err := unmarshalRules([]byte(`{oops`)); err == nil {
		t.Fatal("expected ingress decode error")
	}
	if _, err := unmarshalDNS([]byte(`[oops`)); err == nil {
		t.Fatal("expected dns decode error")
	}
	badModel := rowModel{Ingress: []byte(`{oops`)}
	if _, err := rowFromModel(badModel); err == nil {
		t.Fatal("rowFromModel must surface ingress decode errors")
	}
}

func TestSQLRepoNilCollectionsNormalize(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)
	row := &Row{Name: "bare", CfTunnelID: "cf-bare", AccountID: "acc", //nolint:gosec // test fixture
		CredentialSecretName: "tunnel:cf-bare", Status: StatusCreating}
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("Create with nil ingress/dns: %v", err)
	}
	got, err := repo.Get(ctx, row.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Ingress == nil || len(got.Ingress) != 0 {
		t.Fatalf("ingress should decode to empty slice, got %+v", got.Ingress)
	}
	if got.DNSRecords == nil {
		t.Fatal("dns records should decode to empty map")
	}
	if err := repo.UpdateIngress(ctx, row.ID, nil); err != nil {
		t.Fatalf("UpdateIngress(nil): %v", err)
	}
	if err := repo.UpdateDNSRecords(ctx, row.ID, nil); err != nil {
		t.Fatalf("UpdateDNSRecords(nil): %v", err)
	}
}

func TestRepoPureHelpers(t *testing.T) {
	if _, err := rowFromModel(rowModel{Ingress: []byte(`[]`), DnsRecords: []byte(`{oops`)}); err == nil {
		t.Fatal("rowFromModel must surface dns decode errors")
	}
	sentinel := errors.New("plain failure")
	if got := mapPgxErr(sentinel); !errors.Is(got, sentinel) {
		t.Fatalf("mapPgxErr must pass through non-pg errors, got %v", got)
	}
	if got := mapPgxErr(nil); got != nil {
		t.Fatalf("mapPgxErr(nil) = %v", got)
	}
}
