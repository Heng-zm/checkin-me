-- v5.3: Device webhook idempotency and performance hardening.
-- Safe to run multiple times.

ALTER TABLE device_events ADD COLUMN IF NOT EXISTS external_event_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_device_events_external_id
ON device_events(org_id, device_sn, external_event_id)
WHERE external_event_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_device_events_org_time
ON device_events(org_id, event_at DESC);

CREATE INDEX IF NOT EXISTS idx_attendance_events_source_time
ON attendance_events(org_id, source, event_at DESC);
