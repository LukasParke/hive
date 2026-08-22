alter table applications
  add column ssh_key_id uuid references ssh_keys(id) on delete set null;

create index if not exists idx_applications_ssh_key on applications(ssh_key_id);
