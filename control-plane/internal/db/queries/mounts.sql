-- name: ListMounts :many
select id::text, application_id::text, type, source, target, read_only, created_at
from mounts
where organization_id = @organization_id::uuid
order by created_at desc;

-- name: GetMount :one
select id::text, application_id::text, type, source, target, read_only, created_at
from mounts
where id = @id::uuid and organization_id = @organization_id::uuid;

-- name: CreateMount :one
insert into mounts(organization_id, application_id, type, source, target, read_only)
values (@organization_id::uuid, @application_id::uuid, @type, @source, @target, @read_only)
returning id::text;

-- name: UpdateMount :exec
update mounts
set application_id = @application_id::uuid, type = @type, source = @source, target = @target, read_only = @read_only, updated_at = now()
where id = @id::uuid and organization_id = @organization_id::uuid;

-- name: DeleteMount :exec
delete from mounts
where id = @id::uuid and organization_id = @organization_id::uuid;
