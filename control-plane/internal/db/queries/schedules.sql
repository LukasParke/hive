-- name: GetSchedule :one
select id, name, cron_expr, target_type, target_id, enabled, last_run_at, created_at
from schedules
where id = $1;

-- name: ListSchedules :many
select id, name, cron_expr, target_type, target_id, enabled, last_run_at, created_at
from schedules
order by created_at desc;

-- name: CreateSchedule :one
insert into schedules(name, cron_expr, target_type, target_id, enabled)
values ($1, $2, $3, $4, $5)
returning id, name, cron_expr, target_type, target_id, enabled, last_run_at, created_at;

-- name: UpdateSchedule :exec
update schedules
set name = coalesce(nullif($2, ''), name),
    cron_expr = coalesce(nullif($3, ''), cron_expr),
    target_type = coalesce(nullif($4, ''), target_type),
    target_id = coalesce(nullif($5, ''), target_id),
    enabled = case when $6 then $7 else enabled end
where id = $1;

-- name: DeleteSchedule :exec
delete from schedules where id = $1;

-- name: GetScheduleTarget :one
select target_type, target_id from schedules where id = $1;

-- name: UpdateScheduleLastRun :exec
update schedules set last_run_at = now() where id = $1;
