-- v5.4 QR token image/no-expiry support.
-- Makes QR token expiry optional so owners/admins can create permanent wall QR codes.

ALTER TABLE attendance_qr_tokens ALTER COLUMN expires_at DROP NOT NULL;

DO $$
DECLARE
    c record;
BEGIN
    FOR c IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'attendance_qr_tokens'::regclass
          AND contype = 'c'
          AND pg_get_constraintdef(oid) ILIKE '%expires_at%'
    LOOP
        EXECUTE format('ALTER TABLE attendance_qr_tokens DROP CONSTRAINT IF EXISTS %I', c.conname);
    END LOOP;
END $$;

ALTER TABLE attendance_qr_tokens DROP CONSTRAINT IF EXISTS attendance_qr_tokens_expires_at_valid;

ALTER TABLE attendance_qr_tokens
    ADD CONSTRAINT attendance_qr_tokens_expires_at_valid
    CHECK (expires_at IS NULL OR expires_at > created_at);

CREATE INDEX IF NOT EXISTS idx_qr_tokens_no_expiry
    ON attendance_qr_tokens(org_id, active, created_at DESC)
    WHERE expires_at IS NULL;
