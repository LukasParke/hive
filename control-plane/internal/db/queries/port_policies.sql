-- name: ListPortPolicies :many
select id::text, application_id::text, published_port, target_port, protocol, mode, created_at
from port_policies
where organization_id = @organization_id::uuid
order by created_at desc;

-- name: GetPortPolicy :one
select id::text, application_id::text, published_port, target_port, protocol, mode, created_at
from port_policies
where id = @id::uuid and organization_id = @organization_id::uuid;

-- name: CreatePortPolicy :one
insert into port_policies(organization_id, application_id, published_port, target_port, protocol, mode)
values (@organization_id::uuid, @application_id::uuid, @published_port, @target_port, @protocol, @mode)
returning id::text;

-- name: UpdatePortPolicy :exec
update port_policies
set application_id = @application_id::uuid, published_port = @published_port, target_port = @target_port, protocol = @protocol, mode = @mode, updated_at = now()
where id = @id::uuid and organization_id = @organization_id::uuid;

-- name: DeletePortPolicy :exec
delete from port_policies
where id = @id::uuid and organization_id = @organization_id::uuid;
