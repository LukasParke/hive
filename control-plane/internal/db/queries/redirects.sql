-- name: ListRedirects :many
select id::text, domain_id::text, path, target, status_code, permanent, created_at
from redirects
where organization_id = @organization_id::uuid
order by created_at desc;

-- name: GetRedirect :one
select id::text, domain_id::text, path, target, status_code, permanent, created_at
from redirects
where id = @id::uuid and organization_id = @organization_id::uuid;

-- name: CreateRedirect :one
insert into redirects(organization_id, domain_id, path, target, status_code, permanent)
values (@organization_id::uuid, @domain_id::uuid, @path, @target, @status_code, @permanent)
returning id::text;

-- name: UpdateRedirect :exec
update redirects
set domain_id = @domain_id::uuid, path = @path, target = @target, status_code = @status_code, permanent = @permanent, updated_at = now()
where id = @id::uuid and organization_id = @organization_id::uuid;

-- name: DeleteRedirect :exec
delete from redirects
where id = @id::uuid and organization_id = @organization_id::uuid;
