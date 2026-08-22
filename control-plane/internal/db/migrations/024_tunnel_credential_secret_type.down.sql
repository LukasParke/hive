-- Down migration for enum value removal is a no-op: PostgreSQL cannot drop
-- enum values; existing rows referencing 'tunnel_credential' would block it.
SELECT 1;
