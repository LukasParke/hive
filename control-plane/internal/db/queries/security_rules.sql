-- name: ListSecurityRules :many
select id::text, application_id::text, name, type, config, priority, enabled, created_at, updated_at
from security_rules
where organization_id = @organization_id::uuid
order by priority desc, created_at desc;

-- name: GetSecurityRule :one
select id::text, application_id::text, name, type, config, priority, enabled, created_at, updated_at
from security_rules
where id = @id::uuid and organization_id = @organization_id::uuid;

-- name: CreateSecurityRule :one
insert into security_rules(organization_id, application_id, name, type, config, priority, enabled)
values (@organization_id::uuid, @application_id::uuid, @name, @type, @config, @priority, @enabled)
returning id::text;

-- name: UpdateSecurityRule :exec
update security_rules
set application_id = @application_id::uuid, name = @name, type = @type, config = @config, priority = @priority, enabled = @enabled, updated_at = now()
where id = @id::uuid and organization_id = @organization_id::uuid;

-- name: DeleteSecurityRule :exec
delete from security_rules
where id = @id::uuid and organization_id = @organization_id::uuid;

-- name: ListSecurityRulesByApplication :many
select id::text, name, type, config, priority, enabled, created_at, updated_at
from security_rules
where application_id = @application_id::uuid and organization_id = @organization_id::uuid and enabled = true
order by priority desc, created_at desc;
