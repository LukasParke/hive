-- name: GetUserByEmail :one
select id, email, password_hash, display_name, is_active, created_at
from users
where lower(email) = lower($1);

-- name: GetUserByID :one
select id, email, password_hash, display_name, is_active, created_at
from users
where id = $1;

-- name: CreateUser :one
insert into users(email, password_hash, display_name)
values ($1, $2, $3)
returning id, email, password_hash, display_name, is_active, created_at;

-- name: UpdateUserPassword :exec
update users set password_hash = $2 where id = $1;

-- name: DeleteUserSessions :exec
delete from sessions where user_id = $1;

-- name: UpdateUserProfile :exec
update users set display_name = $2 where id = $1;

-- name: GetUserProfile :one
select id::text, email, display_name, is_active, created_at
from users
where id = $1::uuid;
