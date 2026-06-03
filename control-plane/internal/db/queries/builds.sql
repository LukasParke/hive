-- name: EnqueueBuild :one
insert into build_jobs(application_id, trigger, status, image_tag)
values ($1, $2, 'queued', $3)
returning id, application_id, status, trigger, image_tag, created_at;

-- name: GetBuild :one
select b.id, b.application_id, b.status::text, b.trigger, b.image_tag, b.retries, b.created_at
from build_jobs b
where b.id = $1;

-- name: ListBuildsByOrganization :many
select b.id, b.application_id, b.status::text, b.trigger, b.image_tag, b.created_at
from build_jobs b
join applications a on a.id = b.application_id
join projects p on p.id = a.project_id
where p.organization_id = $1
order by b.created_at desc;

-- name: ListBuildQueueByOrganization :many
select b.id, b.application_id, b.status::text, b.trigger, b.image_tag, b.retries, b.created_at
from build_jobs b
join applications a on a.id = b.application_id
join projects p on p.id = a.project_id
where p.organization_id = $1 and b.status in ('queued', 'building')
order by b.created_at asc;

-- name: CancelBuild :exec
update build_jobs b
set status = 'cancelled'
from applications a, projects p
where b.id = $1 and a.id = b.application_id and p.id = a.project_id and p.organization_id = $2
and b.status in ('queued', 'building');

-- name: GetBuildApplicationID :one
select b.application_id::text
from build_jobs b
join applications a on a.id = b.application_id
join projects p on p.id = a.project_id
where b.id = $1 and p.organization_id = $2;

-- name: GetBuildJobForWorker :one
select id::text, trigger
from build_jobs
where status = 'queued'
order by created_at asc
for update skip locked
limit 1;

-- name: ClaimBuildJob :exec
update build_jobs
set status = 'building', started_at = now()
where id = $1::uuid;

-- name: FailBuildJob :exec
update build_jobs
set status = case when retries < 3 then 'queued' else 'failed' end,
    retries = retries + 1,
    error_message = $2,
    completed_at = case when retries >= 3 then now() else completed_at end
where id = $1::uuid;

-- name: CompleteBuildJob :exec
update build_jobs
set status = 'complete', completed_at = now(), image_tag = $2
where id = $1::uuid;

-- name: AppendBuildLog :exec
update build_jobs
set logs = logs || $2
where id = $1::uuid;

-- name: GetBuildJobDetails :one
select bj.application_id::text, a.source_type::text, bj.trigger, a.name, a.project_id::text,
       coalesce(a.image, ''), coalesce(bj.image_tag, ''), coalesce(a.repository_url, ''), coalesce(a.git_ref, 'main'), coalesce(a.container_port, 3000)
from build_jobs bj
join applications a on a.id = bj.application_id
where bj.id = $1::uuid;

-- name: CountQueuedBuilds :one
select count(*) from build_jobs where status in ('queued', 'building');

-- name: ListDeploymentsByApplication :many
select id, application_id, image_tag, status, trigger, created_at
from deployments
where application_id = $1
order by created_at desc;

-- name: CreateDeployment :exec
insert into deployments(application_id, image_tag, status, trigger)
values ($1, $2, 'deployed', $3);

-- name: GetPreviousDeploymentImageTag :one
select image_tag
from deployments
where application_id = $1
order by created_at desc
offset 1
limit 1;
