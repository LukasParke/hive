-- name: ListServers :many
select id, name, host, ssh_port, description, created_at
from servers
order by created_at desc;

-- name: CreateServer :one
insert into servers(name, host, ssh_port, description)
values ($1, $2, $3, $4)
returning id, name, host, ssh_port, description, created_at;

-- name: GetServer :one
select id, name, host, ssh_port, description, created_at
from servers
where id = $1;

-- name: ListSSHKeys :many
select id, name, public_key, created_at
from ssh_keys
order by created_at desc;

-- name: CreateSSHKey :one
insert into ssh_keys(name, public_key, private_key)
values ($1, $2, $3)
returning id, name, public_key, created_at;

-- name: ListCertificates :many
select id, domain, created_at
from certificates
order by created_at desc;

-- name: UpsertCertificate :one
insert into certificates(domain, cert_pem, key_pem)
values ($1, $2, $3)
on conflict (domain) do update set
    cert_pem = excluded.cert_pem,
    key_pem = excluded.key_pem
returning id, domain, created_at;

-- name: ListRequestEvents :many
select id, category, message, payload, created_at
from request_events
order by created_at desc
limit 200;

-- name: CreateRequestEvent :exec
insert into request_events(category, message, payload)
values ($1, $2, $3::jsonb);
