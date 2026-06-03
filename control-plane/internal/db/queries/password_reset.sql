-- name: CreatePasswordResetToken :exec
insert into password_reset_tokens(user_id, token_hash, expires_at)
values ($1::uuid, $2, now() + interval '1 hour');

-- name: GetValidPasswordResetToken :one
select id::text, user_id::text
from password_reset_tokens
where token_hash = $1 and expires_at > now()
order by created_at desc
limit 1;

-- name: DeletePasswordResetToken :exec
delete from password_reset_tokens where id = $1::uuid;
