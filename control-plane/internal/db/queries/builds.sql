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

-- name: GetBuildStatus :one
select status::text
from build_jobs
where id = sqlc.arg(build_id)::uuid;

-- name: MarkBuildRunning :exec
update build_jobs
set status = 'building', started_at = now(), error_message = null
where id = sqlc.arg(build_id)::uuid;

-- name: MarkBuildFailed :exec
update build_jobs
set status = 'failed', error_message = sqlc.arg(error_message), completed_at = now()
where id = sqlc.arg(build_id)::uuid;

-- name: SetBuildImageTag :exec
update build_jobs set image_tag = sqlc.arg(image_tag) where id = sqlc.arg(build_id)::uuid;

-- name: SetBuildGitSha :exec
update build_jobs set git_sha = sqlc.arg(git_sha) where id = sqlc.arg(build_id)::uuid;

-- name: CompleteBuildJob :exec
update build_jobs
set status = 'complete', completed_at = now(), image_tag = sqlc.arg(image_tag)
where id = sqlc.arg(build_id)::uuid;

-- name: AppendBuildLog :exec
update build_jobs
set logs = logs || sqlc.arg(chunk)
where id = sqlc.arg(build_id)::uuid;

-- name: ResetBuildForRetry :one
update build_jobs b
set status = 'queued', retries = 0, error_message = null,
    started_at = null, completed_at = null
from applications a, projects p
where b.id = sqlc.arg(build_id)::uuid and a.id = b.application_id and p.id = a.project_id
  and p.organization_id = sqlc.arg(organization_id)::uuid and b.status not in ('queued', 'building')
returning b.id::text;

-- name: GetBuildLog :one
select b.logs
from build_jobs b
join applications a on a.id = b.application_id
join projects p on p.id = a.project_id
where b.id = sqlc.arg(build_id)::uuid and p.organization_id = sqlc.arg(organization_id)::uuid;

-- name: GetBuildForExecution :one
select bj.id::text as build_id, bj.status::text as status,
       bj.application_id::text as application_id, a.source_type::text as source_type,
       bj.trigger, a.name, a.project_id::text as project_id,
       coalesce(a.image, '') as image, coalesce(bj.image_tag, '') as requested_image_tag,
       coalesce(a.repository_url, '') as repository_url, coalesce(a.git_ref, 'main') as git_ref,
       coalesce(a.container_port, 3000) as container_port,
       a.registry_id, a.service_spec, coalesce(bj.git_sha, '') as git_sha
from build_jobs bj
join applications a on a.id = bj.application_id
where bj.id = sqlc.arg(build_id)::uuid;

-- name: PruneBuildHistory :exec
delete from build_jobs b
where (
  select count(*)
  from build_jobs newer
  where newer.application_id = b.application_id
    and (newer.created_at > b.created_at
         or (newer.created_at = b.created_at and newer.id::text > b.id::text))
) >= $1::int;

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

-- name: CreatePendingDeployment :one
insert into deployments(application_id, image_tag, status, trigger)
values (sqlc.arg(application_id)::uuid, sqlc.arg(image_tag), 'pending', sqlc.arg(trigger))
returning id::text;

-- name: GetDeploymentForExecution :one
select d.id::text as id, d.application_id::text as application_id,
       d.image_tag, d.trigger, a.name, a.project_id::text as project_id,
       coalesce(a.container_port, 3000) as container_port
from deployments d
join applications a on a.id = d.application_id
where d.id = sqlc.arg(deployment_id)::uuid;

-- name: MarkDeploymentStatus :exec
update deployments set status = sqlc.arg(status) where id = sqlc.arg(deployment_id)::uuid;
