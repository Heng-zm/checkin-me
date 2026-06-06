# Changelog

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
