-- name: ListVolumeBackups :many
select id::text, volume_name, status, size_bytes, destination_id::text, error_message, created_at, completed_at
from volume_backups
where organization_id = @organization_id::uuid
order by created_at desc;

-- name: GetVolumeBackup :one
select id::text, volume_name, status, size_bytes, destination_id::text, error_message, created_at, completed_at
from volume_backups
where id = @id::uuid and organization_id = @organization_id::uuid;

-- name: CreateVolumeBackup :one
insert into volume_backups(organization_id, volume_name, destination_id)
values (@organization_id::uuid, @volume_name, @destination_id::uuid)
returning id::text;

-- name: UpdateVolumeBackupStatus :exec
update volume_backups
set status = @status, size_bytes = @size_bytes, error_message = @error_message, completed_at = @completed_at
where id = @id::uuid;

-- name: DeleteVolumeBackup :exec
delete from volume_backups
where id = @id::uuid and organization_id = @organization_id::uuid;
