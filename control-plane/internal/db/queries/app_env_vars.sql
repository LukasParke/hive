-- name: ListEnvVarsByApplication :many
select ev.id, ev.key, ev.value, ev.is_secret, ev.created_at, ev.updated_at
from app_env_vars ev
join applications a on a.id = ev.application_id
join projects p on p.id = a.project_id
where ev.application_id = $1 and p.organization_id = $2
order by ev.key;

-- name: GetEnvVar :one
select ev.id, ev.key, ev.value, ev.is_secret, ev.secret_version, ev.docker_secret_id, ev.created_at, ev.updated_at
from app_env_vars ev
join applications a on a.id = ev.application_id
join projects p on p.id = a.project_id
where ev.id = $1 and ev.application_id = $2 and p.organization_id = $3;

-- name: CreateEnvVar :one
insert into app_env_vars(application_id, key, value, is_secret)
values ($1, $2, $3, $4)
returning id, key, value, is_secret, created_at, updated_at;

-- name: UpdateEnvVar :exec
update app_env_vars
set value = $2, secret_version = secret_version + 1, updated_at = now()
where id = $1;

-- name: DeleteEnvVar :exec
delete from app_env_vars where id = $1;

-- name: IncrementEnvVarSecretVersion :exec
update app_env_vars
set secret_version = secret_version + 1, updated_at = now()
where id = $1;
