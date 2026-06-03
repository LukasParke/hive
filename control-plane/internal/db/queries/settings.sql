-- name: GetSettings :many
select key, value
from app_settings
order by key;

-- name: UpsertSetting :exec
insert into app_settings(key, value, updated_at)
values ($1, $2::jsonb, now())
on conflict (key) do update set value = excluded.value, updated_at = now();
