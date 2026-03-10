package store

import (
	"context"
	"encoding/json"
	"time"
)

type UPSDevice struct {
	ID                  string          `json:"id"`
	OrgID               string          `json:"org_id"`
	Name                string          `json:"name"`
	NUTHost             string          `json:"nut_host"`
	NUTPort             int             `json:"nut_port"`
	UPSName             string          `json:"ups_name"`
	PollIntervalSeconds int             `json:"poll_interval_seconds"`
	ShutdownThreshold   int             `json:"shutdown_threshold"`
	ShutdownNodes       json.RawMessage `json:"shutdown_nodes"`
	CreatedAt           time.Time       `json:"created_at"`
}

type UPSStatusSnapshot struct {
	ID             string    `json:"id"`
	DeviceID       string    `json:"device_id"`
	Status         string    `json:"status"`
	BatteryCharge  float64   `json:"battery_charge"`
	BatteryRuntime int       `json:"battery_runtime"`
	InputVoltage   float64   `json:"input_voltage"`
	OutputVoltage  float64   `json:"output_voltage"`
	LoadPercent    float64   `json:"load_percent"`
	Temperature    float64   `json:"temperature"`
	Timestamp      time.Time `json:"timestamp"`
}

func (s *Store) CreateUPSDevice(ctx context.Context, orgID, name, host string, port int, upsName string, interval, threshold int, nodes json.RawMessage) (*UPSDevice, error) {
	d := &UPSDevice{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO ups_device (org_id, name, nut_host, nut_port, ups_name, poll_interval_seconds, shutdown_threshold, shutdown_nodes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, org_id, name, nut_host, nut_port, ups_name, poll_interval_seconds, shutdown_threshold, shutdown_nodes, created_at`,
		orgID, name, host, port, upsName, interval, threshold, nodes,
	).Scan(&d.ID, &d.OrgID, &d.Name, &d.NUTHost, &d.NUTPort, &d.UPSName, &d.PollIntervalSeconds, &d.ShutdownThreshold, &d.ShutdownNodes, &d.CreatedAt)
	return d, err
}

func (s *Store) ListUPSDevices(ctx context.Context, orgID string) ([]UPSDevice, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, name, nut_host, nut_port, ups_name, poll_interval_seconds, shutdown_threshold, shutdown_nodes, created_at
		 FROM ups_device WHERE org_id = $1 ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var devices []UPSDevice
	for rows.Next() {
		var d UPSDevice
		if err := rows.Scan(&d.ID, &d.OrgID, &d.Name, &d.NUTHost, &d.NUTPort, &d.UPSName, &d.PollIntervalSeconds, &d.ShutdownThreshold, &d.ShutdownNodes, &d.CreatedAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, nil
}

func (s *Store) GetUPSDevice(ctx context.Context, id string) (*UPSDevice, error) {
	d := &UPSDevice{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, name, nut_host, nut_port, ups_name, poll_interval_seconds, shutdown_threshold, shutdown_nodes, created_at
		 FROM ups_device WHERE id = $1`, id,
	).Scan(&d.ID, &d.OrgID, &d.Name, &d.NUTHost, &d.NUTPort, &d.UPSName, &d.PollIntervalSeconds, &d.ShutdownThreshold, &d.ShutdownNodes, &d.CreatedAt)
	return d, err
}

func (s *Store) UpdateUPSDevice(ctx context.Context, id, name, host string, port int, upsName string, interval, threshold int, nodes json.RawMessage) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ups_device SET name=$2, nut_host=$3, nut_port=$4, ups_name=$5, poll_interval_seconds=$6, shutdown_threshold=$7, shutdown_nodes=$8 WHERE id=$1`,
		id, name, host, port, upsName, interval, threshold, nodes)
	return err
}

func (s *Store) DeleteUPSDevice(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ups_device WHERE id = $1`, id)
	return err
}

func (s *Store) CreateUPSSnapshot(ctx context.Context, deviceID, status string, charge float64, runtime int, inV, outV, load, temp float64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ups_status_snapshot (device_id, status, battery_charge, battery_runtime, input_voltage, output_voltage, load_percent, temperature)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		deviceID, status, charge, runtime, inV, outV, load, temp)
	return err
}

func (s *Store) ListUPSSnapshots(ctx context.Context, deviceID string, limit int) ([]UPSStatusSnapshot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, device_id, status, battery_charge, battery_runtime, input_voltage, output_voltage, load_percent, temperature, timestamp
		 FROM ups_status_snapshot WHERE device_id = $1 ORDER BY timestamp DESC LIMIT $2`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var snaps []UPSStatusSnapshot
	for rows.Next() {
		var s UPSStatusSnapshot
		if err := rows.Scan(&s.ID, &s.DeviceID, &s.Status, &s.BatteryCharge, &s.BatteryRuntime, &s.InputVoltage, &s.OutputVoltage, &s.LoadPercent, &s.Temperature, &s.Timestamp); err != nil {
			return nil, err
		}
		snaps = append(snaps, s)
	}
	return snaps, nil
}

func (s *Store) LatestUPSSnapshot(ctx context.Context, deviceID string) (*UPSStatusSnapshot, error) {
	snap := &UPSStatusSnapshot{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, device_id, status, battery_charge, battery_runtime, input_voltage, output_voltage, load_percent, temperature, timestamp
		 FROM ups_status_snapshot WHERE device_id = $1 ORDER BY timestamp DESC LIMIT 1`, deviceID,
	).Scan(&snap.ID, &snap.DeviceID, &snap.Status, &snap.BatteryCharge, &snap.BatteryRuntime, &snap.InputVoltage, &snap.OutputVoltage, &snap.LoadPercent, &snap.Temperature, &snap.Timestamp)
	return snap, err
}
