-- name: ListPreviewDeployments :many
select id::text, application_id::text, pr_number, branch, commit_sha, status, url, expires_at, created_at
from preview_deployments
where application_id = @application_id::uuid and organization_id = @organization_id::uuid
order by created_at desc;

-- name: GetPreviewDeployment :one
select id::text, application_id::text, pr_number, branch, commit_sha, status, url, expires_at, created_at
from preview_deployments
where id = @id::uuid and application_id = @application_id::uuid and organization_id = @organization_id::uuid;

-- name: CreatePreviewDeployment :one
insert into preview_deployments(organization_id, application_id, pr_number, branch, commit_sha, status, url)
values (@organization_id::uuid, @application_id::uuid, @pr_number, @branch, @commit_sha, @status, @url)
returning id::text;

-- name: UpdatePreviewDeploymentStatus :exec
update preview_deployments
set status = @status, url = @url
where id = @id::uuid;

-- name: DeletePreviewDeployment :exec
delete from preview_deployments
where id = @id::uuid and application_id = @application_id::uuid and organization_id = @organization_id::uuid;

-- name: ExpirePreviewDeployments :exec
update preview_deployments
set status = 'expired'
where expires_at < now() and status != 'expired';
