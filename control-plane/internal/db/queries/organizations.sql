-- name: ListOrganizationsForUser :many
select o.id, o.name, o.slug, om.role::text
from organizations o
join organization_members om on om.organization_id = o.id
where om.user_id = $1
order by o.created_at desc;

-- name: GetOrganizationByID :one
select id, name, slug, created_at
from organizations
where id = $1;

-- name: GetOrganizationBySlug :one
select id, name, slug, created_at
from organizations
where slug = $1;

-- name: CreateOrganization :one
insert into organizations(name, slug)
values ($1, $2)
returning id, name, slug, created_at;

-- name: AddOrganizationMember :exec
insert into organization_members(organization_id, user_id, role)
values ($1, $2, $3::member_role)
on conflict (organization_id, user_id) do update set role = excluded.role;

-- name: ListOrganizationMembers :many
select u.id as user_id, u.email, u.display_name, om.role::text
from organization_members om
join users u on u.id = om.user_id
where om.organization_id = $1
order by om.created_at desc;

-- name: UpdateMemberRole :exec
update organization_members
set role = $3::member_role
where organization_id = $1 and user_id = $2;

-- name: DeleteOrganizationMember :exec
delete from organization_members
where organization_id = $1 and user_id = $2;

-- name: ListOrganizationInvitations :many
select id, organization_id, email, role::text, status, token_hash, created_at
from organization_invitations
where organization_id = $1
order by created_at desc;

-- name: CreateOrganizationInvitation :one
insert into organization_invitations(organization_id, email, role, token_hash, created_by)
values ($1, $2, $3::member_role, $4, $5)
returning id, organization_id, email, role::text, status, token_hash, created_at;

-- name: GetInvitationByToken :one
select id, organization_id, email, role::text, status, token_hash, created_at
from organization_invitations
where token_hash = $1 and status = 'pending' and expires_at > now();

-- name: GetInvitationByID :one
select id, organization_id, email, role::text, status, token_hash, created_at
from organization_invitations
where id = $1;

-- name: AcceptInvitation :exec
update organization_invitations
set status = 'accepted'
where id = $1;

-- name: DeleteInvitation :exec
delete from organization_invitations
where id = $1 and organization_id = $2;

-- name: DeletePendingInvitationsByEmail :exec
delete from organization_invitations
where organization_id = $1 and lower(email) = lower($2) and status = 'pending';
