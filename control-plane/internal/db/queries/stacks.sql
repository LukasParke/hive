-- name: GetStack :one
select s.id, s.project_id, s.name, s.compose_content, s.created_at
from stacks s
join projects p on p.id = s.project_id
where s.id = $1 and p.organization_id = $2;

-- name: ListStacks :many
select s.id, s.project_id, s.name, s.compose_content, s.created_at
from stacks s
join projects p on p.id = s.project_id
where p.organization_id = $1
order by s.created_at desc;

-- name: CreateStack :one
insert into stacks(project_id, name, compose_content)
values ($1, $2, $3)
returning id, project_id, name, compose_content, created_at;

-- name: UpdateStack :exec
update stacks
set compose_content = $2
where id = $1;

-- name: DeleteStack :exec
delete from stacks where id = $1;

-- name: GetStackName :one
select name from stacks where id = $1;
