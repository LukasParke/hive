package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validTestdataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join("testdata", "test-app.yaml")
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test-app.yaml"), data, 0644))

	src2 := filepath.Join("testdata", "second-app.yaml")
	data2, err := os.ReadFile(src2)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "second-app.yaml"), data2, 0644))
	return dir
}

func TestLoadFromDir(t *testing.T) {
	dir := validTestdataDir(t)
	c, err := LoadFromDir(dir)
	require.NoError(t, err)
	assert.Len(t, c.List(), 2)
}

func TestLoadFromDirFields(t *testing.T) {
	dir := validTestdataDir(t)
	c, err := LoadFromDir(dir)
	require.NoError(t, err)

	tmpl, err := c.Get("test-app")
	require.NoError(t, err)
	assert.Equal(t, "test-app", tmpl.Name)
	assert.Equal(t, "A test application", tmpl.Description)
	assert.Equal(t, "testing", tmpl.Category)
	assert.Equal(t, "nginx:latest", tmpl.Image)
	assert.Equal(t, 2, tmpl.Replicas)
	assert.Equal(t, "test.local", tmpl.Domain)
	assert.Len(t, tmpl.DependsOn, 1)
	assert.Equal(t, "postgres", tmpl.DependsOn[0].Type)
}

func TestLoadFromDirDefaultReplicas(t *testing.T) {
	dir := validTestdataDir(t)
	c, err := LoadFromDir(dir)
	require.NoError(t, err)

	tmpl, err := c.Get("second-app")
	require.NoError(t, err)
	assert.Equal(t, 1, tmpl.Replicas, "should default to 1 replica")
}

func TestLoadFromDirEmpty(t *testing.T) {
	dir := t.TempDir()
	c, err := LoadFromDir(dir)
	require.NoError(t, err)
	assert.Empty(t, c.List())
}

func TestLoadFromDirNonExistent(t *testing.T) {
	c, err := LoadFromDir("/nonexistent/path")
	require.NoError(t, err)
	assert.Empty(t, c.List())
}

func TestLoadFromDirInvalid(t *testing.T) {
	dir := t.TempDir()
	data, err := os.ReadFile(filepath.Join("testdata", "invalid.yaml"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.yaml"), data, 0644))

	_, err = LoadFromDir(dir)
	assert.Error(t, err)
}

func TestLoadFromDirSkipsNonYAML(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# hello"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("notes"), 0644)

	c, err := LoadFromDir(dir)
	require.NoError(t, err)
	assert.Empty(t, c.List())
}

func TestList(t *testing.T) {
	dir := validTestdataDir(t)
	c, err := LoadFromDir(dir)
	require.NoError(t, err)
	assert.Len(t, c.List(), 2)
}

func TestGet(t *testing.T) {
	dir := validTestdataDir(t)
	c, err := LoadFromDir(dir)
	require.NoError(t, err)

	tmpl, err := c.Get("test-app")
	require.NoError(t, err)
	assert.Equal(t, "test-app", tmpl.Name)
}

func TestGetNotFound(t *testing.T) {
	dir := validTestdataDir(t)
	c, err := LoadFromDir(dir)
	require.NoError(t, err)

	_, err = c.Get("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
