-- name: GetRegistry :one
select id, name, url, username, secret_name, is_default, created_at
from registries
where id = $1;

-- name: ListRegistries :many
select id, name, url, username, secret_name, is_default, created_at
from registries
order by created_at desc;

-- name: CreateRegistry :one
insert into registries(name, url, username, secret_name, is_default)
values ($1, $2, $3, $4, $5)
returning id, name, url, username, secret_name, is_default, created_at;

-- name: UpdateRegistry :exec
update registries
set name = coalesce(nullif($2, ''), name),
    url = coalesce(nullif($3, ''), url),
    username = coalesce($4, username),
    secret_name = coalesce($5, secret_name),
    is_default = case when $6 then $7 else is_default end
where id = $1;

-- name: DeleteRegistry :exec
delete from registries where id = $1;

-- name: GetRegistryURL :one
select url from registries where id = $1;
