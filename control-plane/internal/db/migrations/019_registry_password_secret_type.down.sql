-- Down migration for enum value removal is a no-op: PostgreSQL cannot drop
-- enum values; existing rows referencing 'registry_password' would block it.
SELECT 1;
