alter table git_providers
  add column if not exists webhook_secret text default '',
  add column if not exists enabled boolean not null default true;

alter table applications
  add column if not exists auto_deploy boolean not null default true;
