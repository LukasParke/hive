package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServiceLabels(t *testing.T) {
	labels := ServiceLabels("my-app", "app.example.com", 8080)

	assert.Equal(t, "true", labels["traefik.enable"])
	assert.Equal(t, "Host(`app.example.com`)", labels["traefik.http.routers.my-app.rule"])
	assert.Equal(t, "websecure", labels["traefik.http.routers.my-app.entrypoints"])
	assert.Equal(t, "letsencrypt", labels["traefik.http.routers.my-app.tls.certresolver"])
	assert.Equal(t, "8080", labels["traefik.http.services.my-app.loadbalancer.server.port"])
}

func TestServiceLabelsPortZero(t *testing.T) {
	labels := ServiceLabels("svc", "svc.local", 0)
	assert.Equal(t, "0", labels["traefik.http.services.svc.loadbalancer.server.port"])
}

func TestServiceLabelsCount(t *testing.T) {
	labels := ServiceLabels("svc", "svc.local", 3000)
	assert.Len(t, labels, 5)
}

func TestMergeLabels(t *testing.T) {
	base := map[string]string{"a": "1", "b": "2"}
	extra := map[string]string{"c": "3", "d": "4"}
	merged := MergeLabels(base, extra)

	assert.Equal(t, "1", merged["a"])
	assert.Equal(t, "2", merged["b"])
	assert.Equal(t, "3", merged["c"])
	assert.Equal(t, "4", merged["d"])
	assert.Len(t, merged, 4)
}

func TestMergeLabelsOverride(t *testing.T) {
	base := map[string]string{"key": "base"}
	extra := map[string]string{"key": "extra"}
	merged := MergeLabels(base, extra)

	assert.Equal(t, "extra", merged["key"])
}

func TestMergeLabelsNilBase(t *testing.T) {
	merged := MergeLabels(nil, map[string]string{"a": "1"})
	assert.Equal(t, "1", merged["a"])
}

func TestMergeLabelsNilExtra(t *testing.T) {
	merged := MergeLabels(map[string]string{"a": "1"}, nil)
	assert.Equal(t, "1", merged["a"])
}

func TestMergeLabelsNilBoth(t *testing.T) {
	merged := MergeLabels(nil, nil)
	assert.NotNil(t, merged)
	assert.Len(t, merged, 0)
}

func TestMergeLabelsDoesNotMutateOriginals(t *testing.T) {
	base := map[string]string{"a": "1"}
	extra := map[string]string{"b": "2"}
	merged := MergeLabels(base, extra)

	merged["c"] = "3"
	assert.NotContains(t, base, "c")
	assert.NotContains(t, extra, "c")
}
