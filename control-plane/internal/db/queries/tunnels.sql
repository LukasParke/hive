-- name: ListTunnels :many
select id::text, name, cf_tunnel_id, account_id, zone_id, credential_secret_name,
       ingress, dns_records, status, error_message, created_at, updated_at
from tunnels
order by created_at desc;

-- name: GetTunnel :one
select id::text, name, cf_tunnel_id, account_id, zone_id, credential_secret_name,
       ingress, dns_records, status, error_message, created_at, updated_at
from tunnels
where id = @id::uuid;

-- name: GetTunnelByName :one
select id::text, name, cf_tunnel_id, account_id, zone_id, credential_secret_name,
       ingress, dns_records, status, error_message, created_at, updated_at
from tunnels
where name = @name;

-- name: CreateTunnel :one
insert into tunnels (name, cf_tunnel_id, account_id, zone_id, credential_secret_name, ingress, dns_records, status)
values (@name, @cf_tunnel_id, @account_id, @zone_id, @credential_secret_name, @ingress, @dns_records, @status)
returning id::text;

-- name: UpdateTunnelIngress :one
update tunnels
set ingress = @ingress, updated_at = now()
where id = @id::uuid
returning id::text;

-- name: UpdateTunnelDNSRecords :exec
update tunnels
set dns_records = @dns_records, updated_at = now()
where id = @id::uuid;

-- name: SetTunnelStatus :exec
update tunnels
set status = @status, error_message = @error_message, updated_at = now()
where id = @id::uuid;

-- name: DeleteTunnel :exec
delete from tunnels
where id = @id::uuid;
