-- Speeds up audit lookups filtered by resource (type + id) and time-ordered
-- pagination over the same key.

create index if not exists idx_audit_log_resource_created
  on audit_log(resource_type, resource_id, created_at);
