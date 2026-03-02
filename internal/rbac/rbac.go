package rbac

type Permission string

const (
	PermViewProject       Permission = "project:view"
	PermManageProject     Permission = "project:manage"
	PermDeployApp         Permission = "app:deploy"
	PermManageApp         Permission = "app:manage"
	PermManageSecrets     Permission = "secret:manage"
	PermManageBackups     Permission = "backup:manage"
	PermManageDNS         Permission = "dns:manage"
	PermManageStorage     Permission = "storage:manage"
	PermManageMembers     Permission = "members:manage"
	PermManageSettings    Permission = "settings:manage"
	PermViewMetrics       Permission = "metrics:view"
	PermManageMaintenance Permission = "maintenance:manage"
)

var allPermissions = []Permission{
	PermViewProject, PermManageProject, PermDeployApp, PermManageApp,
	PermManageSecrets, PermManageBackups, PermManageDNS, PermManageStorage,
	PermManageMembers, PermManageSettings, PermViewMetrics, PermManageMaintenance,
}

var rolePermissions = map[string][]Permission{
	"owner":    allPermissions,
	"admin":    {PermViewProject, PermManageProject, PermDeployApp, PermManageApp, PermManageSecrets, PermManageBackups, PermManageDNS, PermManageMembers, PermManageSettings, PermViewMetrics, PermManageMaintenance},
	"deployer": {PermViewProject, PermDeployApp, PermManageApp, PermManageBackups, PermViewMetrics},
	"viewer":   {PermViewProject, PermViewMetrics},
}

func HasPermission(role string, perm Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

func AllPermissions(role string) []Permission {
	return rolePermissions[role]
}
