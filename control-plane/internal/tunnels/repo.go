package tunnels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
)

// SQLRepo is the sqlc-backed Repository implementation.
type SQLRepo struct {
	Q *dbgen.Queries
}

// NewSQLRepo returns a Repository backed by the given connection pool.
func NewSQLRepo(pool *pgxpool.Pool) *SQLRepo {
	return &SQLRepo{Q: dbgen.New(pool)}
}

// pgtypeText converts an optional string into a nullable pg text.
func pgtypeText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

func textValue(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

// parseUUID parses s into a pgtype.UUID parameter value.
func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, InvalidInput("tunnel id %q is not a valid uuid", s)
	}
	return u, nil
}

// marshalRules serializes ingress rules for the jsonb column.
func marshalRules(rules []IngressRule) ([]byte, error) {
	if rules == nil {
		rules = []IngressRule{}
	}
	return json.Marshal(rules)
}

// unmarshalRules parses the ingress jsonb column defensively.
func unmarshalRules(raw json.RawMessage) ([]IngressRule, error) {
	if len(raw) == 0 {
		return []IngressRule{}, nil
	}
	var rules []IngressRule
	if err := json.Unmarshal(raw, &rules); err != nil {
		return nil, fmt.Errorf("decode tunnel ingress: %w", err)
	}
	return rules, nil
}

// marshalDNS serializes hostname→record-id pairs for the jsonb column.
func marshalDNS(records map[string]string) ([]byte, error) {
	if records == nil {
		records = map[string]string{}
	}
	return json.Marshal(records)
}

// unmarshalDNS parses the dns_records jsonb column defensively.
func unmarshalDNS(raw json.RawMessage) (map[string]string, error) {
	out := map[string]string{}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode tunnel dns records: %w", err)
	}
	return out, nil
}

// rowModel is the field set shared by every generated tunnel query row.
type rowModel struct {
	ID                   string
	Name                 string
	CfTunnelID           string
	AccountID            string
	ZoneID               pgtype.Text
	CredentialSecretName string
	Ingress              json.RawMessage
	DnsRecords           json.RawMessage
	Status               string
	ErrorMessage         pgtype.Text
	CreatedAt            pgtype.Timestamptz
	UpdatedAt            pgtype.Timestamptz
}

// rowFromModel converts a generated row model into the domain Row.
func rowFromModel(m rowModel) (*Row, error) {
	rules, err := unmarshalRules(m.Ingress)
	if err != nil {
		return nil, err
	}
	dns, err := unmarshalDNS(m.DnsRecords)
	if err != nil {
		return nil, err
	}
	row := &Row{
		ID:                   m.ID,
		Name:                 m.Name,
		CfTunnelID:           m.CfTunnelID,
		AccountID:            m.AccountID,
		ZoneID:               textValue(m.ZoneID),
		CredentialSecretName: m.CredentialSecretName,
		Ingress:              rules,
		DNSRecords:           dns,
		Status:               m.Status,
		CreatedAt:            m.CreatedAt.Time,
		UpdatedAt:            m.UpdatedAt.Time,
	}
	if m.ErrorMessage.Valid {
		row.ErrorMessage = m.ErrorMessage.String
	}
	return row, nil
}

// mapPgxErr converts driver-level failures into sentinel errors.
func mapPgxErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	default:
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrConflict
		}
		return err
	}
}

// Create inserts a new tunnel row; unique violations surface as ErrConflict.
func (r *SQLRepo) Create(ctx context.Context, row *Row) error {
	ingress, err := marshalRules(row.Ingress)
	if err != nil {
		return err
	}
	dns, err := marshalDNS(row.DNSRecords)
	if err != nil {
		return err
	}
	id, err := r.Q.CreateTunnel(ctx, dbgen.CreateTunnelParams{
		Name:                 row.Name,
		CfTunnelID:           row.CfTunnelID,
		AccountID:            row.AccountID,
		ZoneID:               pgtypeText(row.ZoneID),
		CredentialSecretName: row.CredentialSecretName,
		Ingress:              ingress,
		DnsRecords:           dns,
		Status:               row.Status,
	})
	if err != nil {
		return mapPgxErr(err)
	}
	row.ID = id
	return nil
}

// Get returns a tunnel by ID.
func (r *SQLRepo) Get(ctx context.Context, id string) (*Row, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return nil, err
	}
	m, err := r.Q.GetTunnel(ctx, uid)
	if err := mapPgxErr(err); err != nil {
		return nil, err
	}
	return rowFromModel(rowModel(m))
}

// GetByName returns a tunnel by its unique name.
func (r *SQLRepo) GetByName(ctx context.Context, name string) (*Row, error) {
	m, err := r.Q.GetTunnelByName(ctx, name)
	if err := mapPgxErr(err); err != nil {
		return nil, err
	}
	return rowFromModel(rowModel(m))
}

// List returns every managed tunnel, newest first.
func (r *SQLRepo) List(ctx context.Context) ([]*Row, error) {
	models, err := r.Q.ListTunnels(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]*Row, 0, len(models))
	for _, m := range models {
		row, err := rowFromModel(rowModel(m))
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// UpdateIngress persists a replacement ingress rule list.
func (r *SQLRepo) UpdateIngress(ctx context.Context, id string, rules []IngressRule) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	ingress, err := marshalRules(rules)
	if err != nil {
		return err
	}
	_, err = r.Q.UpdateTunnelIngress(ctx, dbgen.UpdateTunnelIngressParams{
		ID:      uid,
		Ingress: ingress,
	})
	return mapPgxErr(err)
}

// UpdateDNSRecords persists the hostname→record-id map.
func (r *SQLRepo) UpdateDNSRecords(ctx context.Context, id string, records map[string]string) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	dns, err := marshalDNS(records)
	if err != nil {
		return err
	}
	return r.Q.UpdateTunnelDNSRecords(ctx, dbgen.UpdateTunnelDNSRecordsParams{
		ID:         uid,
		DnsRecords: dns,
	})
}

// SetStatus persists the lifecycle status and optional error message.
func (r *SQLRepo) SetStatus(ctx context.Context, id string, status, errorMessage string) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	return r.Q.SetTunnelStatus(ctx, dbgen.SetTunnelStatusParams{
		ID:           uid,
		Status:       status,
		ErrorMessage: pgtypeText(errorMessage),
	})
}

// Delete removes the tunnel row.
func (r *SQLRepo) Delete(ctx context.Context, id string) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	return r.Q.DeleteTunnel(ctx, uid)
}

// ForgetSecrets removes encrypted secrets_store entries by name.
func (r *SQLRepo) ForgetSecrets(ctx context.Context, names []string) error {
	_, err := r.Q.DeleteSecretsByNames(ctx, names)
	return err
}
