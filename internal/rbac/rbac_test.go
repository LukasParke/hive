package rbac

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOwnerHasAllPermissions(t *testing.T) {
	perms := []Permission{
		PermViewProject, PermManageProject, PermDeployApp, PermManageApp,
		PermManageSecrets, PermManageBackups, PermManageDNS,
		PermManageStorage, PermManageMembers, PermManageSettings,
		PermViewMetrics, PermManageMaintenance,
	}
	for _, p := range perms {
		assert.True(t, HasPermission("owner", p), "owner should have %s", p)
	}
}

func TestViewerPermissions(t *testing.T) {
	assert.True(t, HasPermission("viewer", PermViewProject))
	assert.True(t, HasPermission("viewer", PermViewMetrics))
	assert.False(t, HasPermission("viewer", PermDeployApp))
	assert.False(t, HasPermission("viewer", PermManageProject))
	assert.False(t, HasPermission("viewer", PermManageSecrets))
}

func TestDeployerPermissions(t *testing.T) {
	assert.True(t, HasPermission("deployer", PermViewProject))
	assert.True(t, HasPermission("deployer", PermDeployApp))
	assert.True(t, HasPermission("deployer", PermManageApp))
	assert.True(t, HasPermission("deployer", PermManageBackups))
	assert.True(t, HasPermission("deployer", PermViewMetrics))
	assert.False(t, HasPermission("deployer", PermManageSecrets))
	assert.False(t, HasPermission("deployer", PermManageMembers))
}

func TestAdminPermissions(t *testing.T) {
	assert.True(t, HasPermission("admin", PermManageProject))
	assert.True(t, HasPermission("admin", PermManageSecrets))
	assert.True(t, HasPermission("admin", PermManageMembers))
	assert.False(t, HasPermission("admin", PermManageStorage))
}

func TestUnknownRole(t *testing.T) {
	assert.False(t, HasPermission("unknown", PermViewProject))
	assert.False(t, HasPermission("", PermViewProject))
}
