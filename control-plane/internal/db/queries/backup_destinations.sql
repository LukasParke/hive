-- name: GetBackupDestination :one
select id, name, type, config, created_at
from backup_destinations
where id = $1;

-- name: ListBackupDestinations :many
select id, name, type, config, created_at
from backup_destinations
order by created_at desc;

-- name: CreateBackupDestination :one
insert into backup_destinations(name, type, config)
values ($1, $2, $3::jsonb)
returning id, name, type, config, created_at;

-- name: UpdateBackupDestination :exec
update backup_destinations
set name = coalesce(nullif($2, ''), name),
    type = coalesce(nullif($3, ''), type),
    config = coalesce($4::jsonb, config)
where id = $1;

-- name: DeleteBackupDestination :exec
delete from backup_destinations where id = $1;

-- name: GetBackupDestinationConfig :one
select type, config from backup_destinations where id = $1;
