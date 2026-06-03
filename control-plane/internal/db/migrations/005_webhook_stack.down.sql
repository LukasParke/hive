alter table applications
  drop column if exists auto_deploy;

alter table git_providers
  drop column if exists enabled,
  drop column if exists webhook_secret;
