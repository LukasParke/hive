drop index if exists idx_applications_ssh_key;
alter table applications drop column if exists ssh_key_id;
