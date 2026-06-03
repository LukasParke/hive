-- name: GetDatabaseService :one
select ds.id, ds.project_id, ds.engine, ds.name, ds.version, ds.service_name, ds.username, ds.password_secret_name, ds.database_name, ds.port, ds.created_at
from database_services ds
join projects p on p.id = ds.project_id
where ds.id = $1 and p.organization_id = $2;

-- name: ListDatabaseServicesByOrganization :many
select ds.id, ds.project_id, ds.engine, ds.name, ds.version, ds.service_name, ds.username, ds.password_secret_name, ds.database_name, ds.port, ds.created_at
from database_services ds
join projects p on p.id = ds.project_id
where p.organization_id = $1
order by ds.created_at desc;

-- name: CreateDatabaseService :one
insert into database_services(project_id, engine, name, version, service_name, username, password_secret_name, database_name, port)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
returning id, project_id, engine, name, version, service_name, username, password_secret_name, database_name, port, created_at;
