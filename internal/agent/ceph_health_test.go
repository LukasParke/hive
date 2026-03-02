package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCephStatus_Healthy(t *testing.T) {
	raw := `{
		"fsid": "12345678-1234-1234-1234-123456789abc",
		"health": {
			"status": "HEALTH_OK",
			"checks": {}
		},
		"monmap": {
			"mons": [
				{"name": "mon1", "addr": "10.0.0.1:6789"},
				{"name": "mon2", "addr": "10.0.0.2:6789"},
				{"name": "mon3", "addr": "10.0.0.3:6789"}
			]
		},
		"osdmap": {
			"osdmap": {
				"num_osds": 6,
				"num_up_osds": 6,
				"num_in_osds": 6
			}
		},
		"pgmap": {
			"num_pgs": 128,
			"bytes_total": 6000000000000,
			"bytes_used": 1000000000000,
			"bytes_avail": 5000000000000
		},
		"quorum_names": ["mon1", "mon2", "mon3"]
	}`

	report, err := parseCephStatus(raw, "node-1")
	require.NoError(t, err)

	assert.Equal(t, "12345678-1234-1234-1234-123456789abc", report.FSID)
	assert.Equal(t, "node-1", report.NodeID)
	assert.Equal(t, "HEALTH_OK", report.Health)
	assert.Equal(t, 3, report.MonCount)
	assert.Equal(t, 6, report.OSDTotal)
	assert.Equal(t, 6, report.OSDUp)
	assert.Equal(t, 6, report.OSDIn)
	assert.Equal(t, 128, report.PGCount)
	assert.Equal(t, uint64(6000000000000), report.TotalBytes)
	assert.Equal(t, uint64(1000000000000), report.UsedBytes)
	assert.Equal(t, uint64(5000000000000), report.AvailBytes)
	assert.Len(t, report.MonQuorum, 3)
	assert.Empty(t, report.HealthDetail)
}

func TestParseCephStatus_Degraded(t *testing.T) {
	raw := `{
		"fsid": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		"health": {
			"status": "HEALTH_WARN",
			"checks": {
				"OSD_DOWN": {
					"severity": "HEALTH_WARN",
					"summary": {
						"message": "1 osds down",
						"count": 1
					}
				}
			}
		},
		"monmap": {
			"mons": [
				{"name": "mon1", "addr": "10.0.0.1:6789"}
			]
		},
		"osdmap": {
			"osdmap": {
				"num_osds": 3,
				"num_up_osds": 2,
				"num_in_osds": 3
			}
		},
		"pgmap": {
			"num_pgs": 64,
			"bytes_total": 3000000000000,
			"bytes_used": 500000000000,
			"bytes_avail": 2500000000000
		},
		"quorum_names": ["mon1"]
	}`

	report, err := parseCephStatus(raw, "node-2")
	require.NoError(t, err)

	assert.Equal(t, "HEALTH_WARN", report.Health)
	assert.Equal(t, 2, report.OSDUp)
	assert.Equal(t, 3, report.OSDIn)
	assert.Len(t, report.HealthDetail, 1)
	assert.Contains(t, report.HealthDetail[0], "1 osds down")
}

func TestParseCephStatus_InvalidJSON(t *testing.T) {
	_, err := parseCephStatus("not json", "node-1")
	assert.Error(t, err)
}
