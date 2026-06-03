-- name: GetNotification :one
select id, channel, target, enabled, created_at
from notifications
where id = $1;

-- name: ListNotifications :many
select id, channel, target, enabled, created_at
from notifications
order by created_at desc;

-- name: CreateNotification :one
insert into notifications(channel, target, enabled)
values ($1, $2, $3)
returning id, channel, target, enabled, created_at;

-- name: UpdateNotification :exec
update notifications
set channel = coalesce(nullif($2, ''), channel),
    target = coalesce(nullif($3, ''), target),
    enabled = coalesce($4, enabled)
where id = $1;

-- name: DeleteNotification :exec
delete from notifications where id = $1;

-- name: GetNotificationChannelTarget :one
select channel, target from notifications where id = $1;
