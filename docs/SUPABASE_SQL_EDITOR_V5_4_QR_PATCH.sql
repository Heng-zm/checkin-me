-- CheckinMe v5.4 QR patch for Supabase SQL Editor.
-- Use this if AUTO_MIGRATE=false, or if you want to patch the DB before redeploy.

create schema if not exists checkinme;
set search_path to checkinme, public;

alter table if exists attendance_qr_tokens alter column expires_at drop not null;

do $$
declare
    c record;
begin
    if to_regclass('attendance_qr_tokens') is not null then
        for c in
            select conname
            from pg_constraint
            where conrelid = 'attendance_qr_tokens'::regclass
              and contype = 'c'
              and pg_get_constraintdef(oid) ilike '%expires_at%'
        loop
            execute format('alter table attendance_qr_tokens drop constraint if exists %I', c.conname);
        end loop;
    end if;
end $$;

alter table if exists attendance_qr_tokens
    drop constraint if exists attendance_qr_tokens_expires_at_valid;

alter table if exists attendance_qr_tokens
    add constraint attendance_qr_tokens_expires_at_valid
    check (expires_at is null or expires_at > created_at);

create index if not exists idx_qr_tokens_no_expiry
    on attendance_qr_tokens(org_id, active, created_at desc)
    where expires_at is null;

select 'CheckinMe v5.4 QR patch applied' as status;
