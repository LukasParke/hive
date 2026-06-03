-- name: CreateSession :one
insert into sessions(user_id, refresh_token_hash, expires_at)
values ($1, $2, $3)
returning id, user_id, refresh_token_hash, expires_at, created_at;

-- name: GetSessionByRefreshToken :one
select id, user_id, refresh_token_hash, expires_at, created_at
from sessions
where refresh_token_hash = $1;

-- name: DeleteSessionByRefreshToken :exec
delete from sessions where refresh_token_hash = $1;

-- name: DeleteExpiredSessions :exec
delete from sessions where expires_at < now();
