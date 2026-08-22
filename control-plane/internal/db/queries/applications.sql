-- name: GetApplication :one
select a.id, a.project_id, a.name, a.source_type::text, a.image, a.repository_url, a.git_ref, a.container_port, a.watch_paths, a.created_at,
       a.registry_id, r.name as registry_name, r.url as registry_url
from applications a
join projects p on p.id = a.project_id
left join registries r on r.id = a.registry_id
where a.id = $1 and p.organization_id = $2;

-- name: ListApplicationsByProject :many
select a.id, a.project_id, a.name, a.source_type::text, a.image, a.repository_url, a.git_ref, a.container_port, a.watch_paths, a.created_at,
       a.registry_id, r.name as registry_name, r.url as registry_url
from applications a
left join registries r on r.id = a.registry_id
where a.project_id = $1
order by a.created_at desc;

-- name: ListApplicationsByOrganization :many
select a.id, a.project_id, a.name, a.source_type::text, a.image, a.repository_url, a.git_ref, a.container_port, a.watch_paths, a.created_at,
       a.registry_id, r.name as registry_name, r.url as registry_url
from applications a
join projects p on p.id = a.project_id
left join registries r on r.id = a.registry_id
where p.organization_id = $1
order by a.created_at desc;

-- name: CreateApplication :one
insert into applications(project_id, name, source_type, image, repository_url, git_ref, container_port, watch_paths)
values ($1, $2, $3::source_type, $4, $5, $6, $7, $8)
returning id, project_id, name, source_type::text, image, repository_url, git_ref, container_port, watch_paths, created_at;

-- name: UpdateApplication :exec
update applications a
set name = coalesce(nullif($3, ''), a.name),
    image = coalesce($4, a.image),
    repository_url = coalesce($5, a.repository_url),
    git_ref = coalesce($6, a.git_ref),
    container_port = case when $7 > 0 then $7 else a.container_port end
from projects p
where a.id = $1 and p.id = a.project_id and p.organization_id = $2;

-- name: DeleteApplication :exec
delete from applications a
using projects p
where a.id = $1 and p.id = a.project_id and p.organization_id = $2;

-- name: CountApplications :one
select count(*) from applications;

-- name: SetApplicationRegistry :exec
update applications a
set registry_id = $3
from projects p
where a.id = $1 and p.id = a.project_id and p.organization_id = $2;

-- name: GetApplicationSSHKey :one
-- Resolves the application's linked SSH key for private repo clones.
-- No row when the application has no ssh_key_id (sqlc emits :one; callers
-- treat pgx.ErrNoRows as "no key configured").
select k.id::text as key_id, k.name, k.private_key
from applications a
join ssh_keys k on k.id = a.ssh_key_id
where a.id = $1::uuid;

-- name: GetProjectNameForApplication :one
-- Project name doubles as the project-network slug (sanitized by the
-- network manager when creating hive_project_{slug}).
select p.name
from applications a
join projects p on p.id = a.project_id
where a.id = $1::uuid;
