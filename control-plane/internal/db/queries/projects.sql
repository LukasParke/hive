-- name: GetProject :one
select p.id, p.name, p.organization_id, p.created_at
from projects p
where p.id = $1;

-- name: GetProjectByOrg :one
select p.id, p.name, p.organization_id, p.created_at
from projects p
where p.id = $1 and p.organization_id = $2;

-- name: ListProjectsByOrganization :many
select id, name, organization_id, created_at
from projects
where organization_id = $1
order by created_at desc;

-- name: CreateProject :one
insert into projects(name, organization_id)
values ($1, $2)
returning id, name, organization_id, created_at;

-- name: UpdateProject :exec
update projects
set name = $3
where id = $1 and organization_id = $2;

-- name: DeleteProject :exec
delete from projects
where id = $1 and organization_id = $2;
