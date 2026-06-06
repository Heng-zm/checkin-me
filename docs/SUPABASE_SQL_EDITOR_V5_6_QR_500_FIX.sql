-- CheckinMe v5.6 QR Token 500 Fix
-- Paste into Supabase SQL Editor and run if POST /api/v1/attendance/qr-tokens returns 500.
-- Safe: does not delete data.

set search_path to checkinme, public;

create extension if not exists pgcrypto;

alter table if exists attendance_qr_tokens
  alter column expires_at drop not null;

DO $$
DECLARE
    c record;
    rel regclass;
BEGIN
    rel := to_regclass('attendance_qr_tokens');
    IF rel IS NULL THEN
        RAISE NOTICE 'attendance_qr_tokens table not found. Redeploy the latest API with AUTO_MIGRATE=true first.';
        RETURN;
    END IF;

    FOR c IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = rel
          AND contype = 'c'
          AND pg_get_constraintdef(oid) ILIKE '%expires_at%'
    LOOP
        EXECUTE format('ALTER TABLE attendance_qr_tokens DROP CONSTRAINT IF EXISTS %I', c.conname);
    END LOOP;
END $$;

alter table if exists attendance_qr_tokens
  add constraint attendance_qr_tokens_expires_at_valid
  check (expires_at is null or expires_at > created_at);

create index if not exists idx_qr_tokens_org_active_created
  on attendance_qr_tokens(org_id, active, created_at desc);

create index if not exists idx_qr_tokens_org_active_expiry
  on attendance_qr_tokens(org_id, active, expires_at);

select
  table_schema,
  table_name,
  column_name,
  is_nullable
from information_schema.columns
where table_schema = 'checkinme'
  and table_name = 'attendance_qr_tokens'
  and column_name = 'expires_at';
