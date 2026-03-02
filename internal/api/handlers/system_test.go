package handlers

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSystemStatusResponseJSON(t *testing.T) {
	resp := SystemStatusResponse{
		Status:    "healthy",
		Role:      "manager",
		NodeCount: 3,
		MultiNode: true,
		NATS:      "CONNECTED",
	}
	data, err := json.Marshal(resp)
	assert.NoError(t, err)

	var decoded SystemStatusResponse
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "healthy", decoded.Status)
	assert.Equal(t, 3, decoded.NodeCount)
	assert.True(t, decoded.MultiNode)
}

func TestWriteJSON(t *testing.T) {
	// writeJSON is tested indirectly through all other handler tests
	// but we verify the response struct serialization here
	resp := SystemStatusResponse{
		Status:    "healthy",
		Role:      "worker",
		NodeCount: 1,
		MultiNode: false,
		NATS:      "DISCONNECTED",
	}
	data, _ := json.Marshal(resp)
	assert.Contains(t, string(data), `"role":"worker"`)
	assert.Contains(t, string(data), `"multi_node":false`)
}
