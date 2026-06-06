# Changelog

## v5.4 - QR Image Generation and No-Expiry QR Tokens

- Fixed QR token generation so the API now returns a scan-ready PNG QR image.
- Added `qr_image_data_url`, `qr_image_base64`, `qr_content`, and QR metadata to the create QR token response.
- Added configurable QR image size through `qr_size_px` with safe bounds.
- Removed the old 24-hour QR TTL cap.
- Added `expires_at` support for owner/admin-selected exact expiry times.
- Added `no_expiry` / `unlimited` support for permanent wall QR codes.
- Updated QR validation so tokens with `expires_at=null` never expire while still requiring active status and GPS rules.
- Added migration `0003_v5_4_qr_image_no_expiry.sql`.
- Added `docs/SUPABASE_SQL_EDITOR_V5_4_QR_PATCH.sql`.


## v5.2 - Supabase schema isolation fix
- Added `DB_SCHEMA=checkinme` support.
- App connections now set `search_path` to the configured schema.
- Startup creates the schema automatically before migrations.
- Fixes Supabase projects that already have `public.users` or other existing app tables.


## v4 - Attendance Anti-Fraud V2 + Performance V2

### Added
- Attendance Anti-Fraud V2 scoring engine for mobile, GPS, QR, and face-scan clocking.
- Fraud statuses: `normal`, `warning`, `needs_review`, `blocked`, `reviewed`, `false_positive`, `confirmed`.
- Fraud signals for mock GPS, poor GPS accuracy, QR replay, duplicate clock events, borderline face scores, missing GPS evidence, and impossible travel speed.
- Manager fraud alert APIs:
  - `GET /api/v1/attendance/fraud-alerts`
  - `PATCH /api/v1/attendance/fraud-alerts/{id}/review`
- Fraud fields in attendance event/session responses.
- Configurable fraud thresholds through `.env`.
- Performance V2 request timeout middleware.
- Slow request logging.
- Bounded async worker queue for Telegram/report background work.
- In-memory TTL cache for dashboard/report summary and Telegram chat ID lookups.
- `GET /api/v1/system/performance` for PostgreSQL pool, cache, and async worker stats.
- Pagination for customers, sales visits, and attendance sessions.
- New database indexes for attendance/report/fraud queries.

### Fixed
- Fixed duplicate `user_id` column in `payroll_items` migration.
- Improved report summary by using one SQL round trip instead of multiple separate queries.
- Invalidates dashboard cache after attendance, employee, sales, and device attendance changes.

### Notes
- The memory cache is safe for a single API instance. For multi-instance production, keep the TTL short or replace `internal/cache` with Redis using the same cache interface.
- Blocked fraud attempts are stored in `audit_logs` and do not create attendance sessions.

## v3 - Stability and validation
- Fixed payroll generation cursor/connection risk.
- Added stronger validations, panic recovery, security headers, and pagination.

## v2 - Product module expansion
- Added QR/GPS/face attendance, schedule builder, payroll exports, EWA, bank batch workflow, and sales route summaries.


## v5 Supabase Deployment

- Changed Render Blueprint to use external Supabase Postgres instead of Render Postgres.
- Added `.env.supabase.example`.
- Added `docs/SUPABASE_DEPLOY.md`.
- Added `DB_QUERY_EXEC_MODE` for Supabase transaction-pooler compatibility.
- Reduced default DB pool size for Supabase-friendly deployment.

## v5.1 Render build checksum fix

- Updated Dockerfile to build with `go build -mod=mod` so Render can resolve missing `go.sum` entries during Docker builds.
- Added `docs/RENDER_BUILD_FIX.md`.

## v5.3 - API Optimization, Device Webhook Compatibility, and Performance Hardening

- Added built-in per-IP token-bucket rate limiting with Render proxy header support.
- Added `RATE_LIMIT_ENABLED`, `RATE_LIMIT_REQUESTS_PER_MINUTE`, and `RATE_LIMIT_BURST` settings.
- Added rate-limit visibility to `/api/v1/system/performance`.
- Added `/ready` health alias for platform readiness checks.
- Added legacy-compatible `/api/v1/device/face-webhook` route in addition to `/api/v1/devices/face-events`.
- Device webhooks now accept both `X-Device-Webhook-Secret` and `X-Device-Secret` headers.
- Device webhook secret comparison now uses constant-time comparison.
- Face-device webhooks can identify employees by `employee_code` or `user_id`.
- Added `external_event_id` idempotency support for face-device webhooks to prevent duplicate punches.
- Added face-device fraud flags for missing, low, or borderline face scores.
- Added migration advisory lock to prevent concurrent Render instances from applying migrations at the same time.
- Added `device_events.external_event_id` and unique idempotency index.
- Hardened Dockerfile for Render builds, smaller binary output, and non-root runtime user.
