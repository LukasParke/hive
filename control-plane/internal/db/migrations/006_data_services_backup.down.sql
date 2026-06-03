alter table backup_runs
  drop column if exists restore_target,
  drop column if exists schedule,
  drop column if exists error_message,
  drop column if exists artifact_path;

drop table if exists database_services;
