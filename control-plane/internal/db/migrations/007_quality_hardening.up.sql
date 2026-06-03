alter table build_jobs
  add column if not exists retries integer not null default 0;
