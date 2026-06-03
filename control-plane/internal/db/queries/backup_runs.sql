-- name: ListBackupRuns :many
select id, target_type, target_id, destination_id, status, artifact_path, error_message, schedule, created_at
from backup_runs
order by created_at desc;

-- name: CreateBackupRun :one
insert into backup_runs(target_type, target_id, destination_id, status, schedule)
values ($1, $2, $3, $4, $5)
returning id, target_type, target_id, destination_id, status, created_at;
