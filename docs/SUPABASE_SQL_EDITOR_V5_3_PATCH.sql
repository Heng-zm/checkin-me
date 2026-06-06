-- Optional manual patch for Supabase SQL Editor.
-- Use this only if AUTO_MIGRATE=false or you want to prepare the DB before redeploy.

create schema if not exists checkinme;
set search_path to checkinme, public;

alter table if exists device_events add column if not exists external_event_id text;

create unique index if not exists idx_device_events_external_id
on device_events(org_id, device_sn, external_event_id)
where external_event_id is not null;

create index if not exists idx_device_events_org_time
on device_events(org_id, event_at desc);

create index if not exists idx_attendance_events_source_time
on attendance_events(org_id, source, event_at desc);
