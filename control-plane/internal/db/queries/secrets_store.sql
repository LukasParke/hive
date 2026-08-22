-- name: GetSecretByName :one
select id, name, type::text, encrypted_value, created_at, updated_at
from secrets_store
where name = $1;

-- name: ListSecrets :many
select id, name, type::text, created_at, updated_at
from secrets_store
order by created_at desc;

-- name: UpsertSecret :one
insert into secrets_store(name, type, encrypted_value)
values ($1, $2::secret_type, $3)
on conflict (name) do update set
    type = excluded.type,
    encrypted_value = excluded.encrypted_value,
    updated_at = now()
returning id, name, type::text, created_at, updated_at;

-- name: DeleteSecretsByNames :execrows
delete from secrets_store
where name = any($1::text[]);
