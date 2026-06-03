-- name: ListAPIKeysByUser :many
select id, name, created_at
from api_keys
where user_id = $1
order by created_at desc;

-- name: CreateAPIKey :one
insert into api_keys(user_id, name, token_hash)
values ($1, $2, $3)
returning id, name, created_at;

-- name: DeleteAPIKey :exec
delete from api_keys where id = $1;
