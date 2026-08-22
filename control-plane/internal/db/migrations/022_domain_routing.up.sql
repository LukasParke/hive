alter table domains add column route_type text not null default 'host' check (route_type in ('host', 'wildcard', 'path'));
alter table domains add column path_prefix text not null default '';
alter table domains add column strip_prefix boolean not null default false;
alter table domains add column priority integer;
