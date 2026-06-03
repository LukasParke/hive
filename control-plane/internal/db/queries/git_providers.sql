-- name: GetGitProvider :one
select id, type, name, base_url, token_secret_name, webhook_secret, enabled, created_at
from git_providers
where id = $1;

-- name: ListGitProviders :many
select id, type, name, base_url, token_secret_name, webhook_secret, enabled, created_at
from git_providers
order by created_at desc;

-- name: CreateGitProvider :one
insert into git_providers(type, name, base_url, token_secret_name, webhook_secret, enabled)
values ($1, $2, $3, $4, $5, $6)
returning id, type, name, base_url, token_secret_name, webhook_secret, enabled, created_at;

-- name: GetGitProviderSecrets :many
select id, webhook_secret from git_providers where type = $1;
