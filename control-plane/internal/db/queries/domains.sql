-- name: GetDomain :one
select d.id, d.application_id, d.hostname, d.tls_enabled, d.created_at
from domains d
join applications a on a.id = d.application_id
join projects p on p.id = a.project_id
where d.id = $1 and p.organization_id = $2;

-- name: ListDomains :many
select d.id, d.application_id, d.hostname, d.tls_enabled, d.created_at
from domains d
join applications a on a.id = d.application_id
join projects p on p.id = a.project_id
where p.organization_id = $1
order by d.created_at desc;

-- name: ListDomainsByApplication :many
select id, application_id, hostname, tls_enabled, created_at
from domains
where application_id = $1;

-- name: CreateDomain :one
insert into domains(application_id, hostname, tls_enabled)
values ($1, $2, $3)
returning id, application_id, hostname, tls_enabled, created_at;

-- name: UpdateDomain :exec
update domains d
set hostname = coalesce(nullif($3, ''), d.hostname),
    tls_enabled = case when $4 then $5 else d.tls_enabled end
from applications a
join projects p on p.id = a.project_id
where d.id = $1 and a.id = d.application_id and p.organization_id = $2;

-- name: DeleteDomain :exec
delete from domains d
using applications a, projects p
where d.id = $1 and a.id = d.application_id and p.id = a.project_id and p.organization_id = $2;

-- name: GetApplicationIDForDomain :one
select application_id from domains where id = $1;
