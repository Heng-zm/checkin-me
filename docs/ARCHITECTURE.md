# CheckinMe Architecture Notes

## Backend modules

1. Auth and role middleware
2. Organization, branches, departments, employees
3. Shift templates and schedule assignments
4. Attendance events and attendance sessions
5. QR attendance token issuing and validation
6. GPS geofence validation
7. Leave and overtime approvals
8. CRM customers and GPS sales visits
9. KPI targets
10. Payroll rules, payroll runs, payroll items, and payslip data
11. Bank accounts, bank transfer batches, and transfer items
12. Earned Wage Access requests and approvals
13. Face-device webhook event ingestion
14. Telegram notification service

## Bank integration boundary

The API creates internal bank transfer batches. It does not submit real money movement unless a bank-approved adapter is added. This is intentional because live bank payout APIs require:

- signed bank contract
- sandbox credentials
- callback/webhook signing details
- idempotency rules
- bank-side payout status lifecycle
- audit logging and reconciliation process

Keep `BANK_PROVIDER=manual_csv` until those details are available.

## Biometric boundary

The API accepts face-device attendance events and face confidence scores. It does not store raw face images or biometric templates. A production build should store any templates only in a biometric vendor device/cloud with encryption and user consent.

## Reliability and performance design in v3

- All long-running DB operations use request-scoped context timeouts.
- Payroll loads employees into memory before running per-employee calculations to keep pgx connections free from nested row-cursor queries.
- Database connection pool size is configurable with `DB_MAX_CONNS`, `DB_MIN_CONNS`, and `DB_MAX_CONN_IDLE_MINUTES`.
- Employee list supports pagination and search to avoid unbounded admin dashboard queries.
- Dashboard/report paths have supporting indexes in the migration.
- API JSON decoder rejects oversized or multi-object bodies.
- Panic recovery converts unexpected handler panics into JSON 500 responses instead of crashing the process.

## Production hardening checklist

- Set `APP_ENV=production`.
- Use a strong `JWT_SECRET` with at least 32 random characters.
- Use a strong `DEVICE_WEBHOOK_SECRET` with at least 24 random characters.
- Set precise `CORS_ALLOWED_ORIGINS`; do not use public wildcards for admin apps.
- Run `go test ./...` and database migration tests before deploy.
- Add object storage for exports if files need to be persisted.
- Add bank adapter only after receiving official bank API documentation.
