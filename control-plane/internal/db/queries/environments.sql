-- name: GetEnvironment :one
select e.id, e.project_id, e.name, e.slug, e.created_at
from environments e
join projects p on p.id = e.project_id
where e.id = $1 and p.organization_id = $2;

-- name: ListEnvironmentsByProject :many
select e.id, e.project_id, e.name, e.slug, e.created_at
from environments e
join projects p on p.id = e.project_id
where e.project_id = $1 and p.organization_id = $2
order by e.created_at desc;

-- name: CreateEnvironment :one
insert into environments(project_id, name, slug)
values ($1, $2, $3)
returning id, project_id, name, slug, created_at;

-- name: UpdateEnvironment :exec
update environments e
set name = coalesce(nullif($3, ''), e.name),
    slug = coalesce(nullif($4, ''), e.slug)
from projects p
where e.id = $1 and p.id = e.project_id and p.organization_id = $2;

-- name: DeleteEnvironment :exec
delete from environments e
using projects p
where e.id = $1 and p.id = e.project_id and p.organization_id = $2;
