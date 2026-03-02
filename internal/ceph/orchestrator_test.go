package ceph

import (
	"testing"

	"github.com/lholliger/hive/internal/agent"
	"github.com/stretchr/testify/assert"
)

func TestExtractFSIDFromConf(t *testing.T) {
	conf := `[global]
fsid = 12345678-1234-1234-1234-123456789abc
mon_initial_members = node1
mon_host = 10.0.0.1
`
	fsid := extractFSIDFromConf(conf)
	assert.Equal(t, "12345678-1234-1234-1234-123456789abc", fsid)
}

func TestExtractFSIDFromConf_Empty(t *testing.T) {
	assert.Equal(t, "", extractFSIDFromConf(""))
	assert.Equal(t, "", extractFSIDFromConf("[global]\nmon_host = 10.0.0.1"))
}

func TestErrorMsg_WithError(t *testing.T) {
	msg := errorMsg(assert.AnError, nil)
	assert.Contains(t, msg, "assert.AnError")
}

func TestErrorMsg_WithResponse(t *testing.T) {
	resp := &agent.CephCommandResponse{Error: "bootstrap failed"}
	msg := errorMsg(nil, resp)
	assert.Equal(t, "bootstrap failed", msg)
}

func TestErrorMsg_WithOutput(t *testing.T) {
	resp := &agent.CephCommandResponse{Output: "some output"}
	msg := errorMsg(nil, resp)
	assert.Equal(t, "some output", msg)
}

func TestErrorMsg_NilAll(t *testing.T) {
	msg := errorMsg(nil, nil)
	assert.Equal(t, "unknown error", msg)
}

func TestFindNode(t *testing.T) {
	nodes := []NodeSelection{
		{NodeID: "n1", Hostname: "host1", IP: "10.0.0.1"},
		{NodeID: "n2", Hostname: "host2", IP: "10.0.0.2"},
	}

	found := findNode(nodes, "n2")
	assert.NotNil(t, found)
	assert.Equal(t, "host2", found.Hostname)

	notFound := findNode(nodes, "n3")
	assert.Nil(t, notFound)
}
