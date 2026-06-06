-- v5.6 QR token repair/performance patch.
-- This makes QR token generation safe on existing Supabase databases that were created before v5.4.

ALTER TABLE attendance_qr_tokens ALTER COLUMN expires_at DROP NOT NULL;

DO $$
DECLARE
    c record;
    rel regclass;
BEGIN
    rel := to_regclass('attendance_qr_tokens');
    IF rel IS NULL THEN
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

ALTER TABLE attendance_qr_tokens
    ADD CONSTRAINT attendance_qr_tokens_expires_at_valid
    CHECK (expires_at IS NULL OR expires_at > created_at);

CREATE INDEX IF NOT EXISTS idx_qr_tokens_org_active_created
    ON attendance_qr_tokens(org_id, active, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_qr_tokens_org_active_expiry
    ON attendance_qr_tokens(org_id, active, expires_at);
