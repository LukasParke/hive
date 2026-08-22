package build

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/luke/hive/control-plane/internal/secrets"
)

// RegistryAuth holds resolved push credentials for a registry.
type RegistryAuth struct {
	Host     string
	Username string
	Password string
}

// ImageRef renders the full image reference for a build:
// {registry}/{project-slug}/{app-slug}:{tag}.
func (r RegistryAuth) ImageRef(project, app, tag string) string {
	return fmt.Sprintf("%s/%s/%s:%s", strings.TrimSuffix(r.Host, "/"), project, app, tag)
}

// SameRegistryHost compares two registry hosts ignoring scheme and trailing
// slash, and treats docker.io / index.docker.io / registry-1.docker.io as
// equivalent.
func SameRegistryHost(a, b string) bool {
	na := normalizeRegistryHost(a)
	nb := normalizeRegistryHost(b)
	if na == nb {
		return true
	}
	dockerHub := map[string]bool{"docker.io": true, "index.docker.io": true, "registry-1.docker.io": true}
	return dockerHub[na] && dockerHub[nb]
}

func normalizeRegistryHost(host string) string {
	host = strings.TrimSuffix(strings.TrimPrefix(host, "https://"), "/")
	host = strings.TrimSuffix(strings.TrimPrefix(host, "http://"), "/")
	return strings.ToLower(host)
}

// ResolveRegistry determines the registry a build should push to:
// the application's pinned registry first, then the default registry,
// then the internal registry address as final fallback. Credentials are
// decrypted from the secrets store when the registry row references a
// secret name.
func ResolveRegistry(ctx context.Context, pool *pgxpool.Pool, appRegistryID *string, internalAddr string) (RegistryAuth, error) {
	var (
		host       string
		username   string
		secretName string
	)
	const cols = `coalesce(url, ''), coalesce(username, ''), coalesce(secret_name, '')`

	if appRegistryID != nil && *appRegistryID != "" {
		err := pool.QueryRow(ctx, fmt.Sprintf(`select %s from registries where id = $1::uuid`, cols), *appRegistryID).
			Scan(&host, &username, &secretName)
		if err != nil {
			return RegistryAuth{}, fmt.Errorf("load application registry %s: %w", *appRegistryID, err)
		}
	}
	if host == "" {
		// Fall back to the default registry, then the internal one.
		err := pool.QueryRow(ctx, fmt.Sprintf(`select %s from registries where is_default = true order by created_at limit 1`, cols)).
			Scan(&host, &username, &secretName)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return RegistryAuth{}, fmt.Errorf("load default registry: %w", err)
		}
	}
	if host == "" {
		host = internalAddr
	}

	auth := RegistryAuth{Host: host, Username: username}
	if secretName != "" {
		store := secrets.Runtime()
		if store == nil {
			return RegistryAuth{}, fmt.Errorf("registry %q references secret %q but the secrets store is not configured", host, secretName)
		}
		password, err := store.Get(ctx, secretName, "registry_password")
		if err != nil {
			return RegistryAuth{}, fmt.Errorf("decrypt registry credentials for %q: %w", host, err)
		}
		auth.Password = string(password)
	}
	return auth, nil
}
