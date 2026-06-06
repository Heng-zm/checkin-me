# CheckinMe System API — Go

Production-style REST API starter for a CheckinMe-style HR, attendance, field-sales, payroll, payout, and Earned Wage Access system.

## Deploy to Render

This package includes `render.yaml` for Render deployment with Docker + Supabase Postgres. See `docs/RENDER_DEPLOY.md` and `docs/SUPABASE_DEPLOY.md`.

Quick test after deploy:

```bash
curl https://YOUR_RENDER_URL.onrender.com/health
```


## Included modules

- **Smart Attendance System**
  - Mobile clock-in / clock-out
  - GPS geofence verification against branch radius
  - Localized QR token clocking
  - QR token generation now returns a scan-ready PNG image/data URL for dashboards
  - Owner/admin configurable QR expiry, including permanent no-expiry wall QR codes
  - AI face-scan compatible clocking
  - Face-device webhook support
  - Telegram attendance alerts and daily reports

- **Shift and Schedule Builder**
  - Branches
  - Departments
  - Shift templates
  - User or department schedule assignments

- **Leave and Overtime Workflow**
  - Employee leave request
  - Employee overtime request
  - Manager/admin approve or reject workflow

- **CRM and Outside Sales Tracker**
  - Customer records
  - GPS-verified customer visit check-in/out
  - KPI targets by employee and month
  - Daily sales route summary
  - Telegram sales field summary

- **Automated Payroll**
  - Attendance-based late deductions
  - Approved overtime pay
  - Unpaid leave deductions
  - Configurable Cambodia salary tax and NSSF rules
  - EWA deduction from monthly payroll
  - Payroll CSV export
  - Bank statement CSV export
  - Digital payslip API

- **Salary Disbursement and EWA**
  - Employee bank account records
  - Payroll bank transfer batch builder
  - Manual CSV mode by default
  - Bank-provider adapter fields for later SmartBiz/API Suite integration
  - Employee EWA request and approval workflow

## Important compliance note

Tax, NSSF, bank payout, and EWA rules can change. This starter stores payroll percentages and exchange rates in `payroll_rules` so finance/admin users can update rates without code changes. Verify production values with your accountant, legal advisor, bank, and the current Cambodian government/NSSF/GDT guidance before processing salary.

## Stack

- Go 1.22+
- PostgreSQL 16+ or Supabase Postgres
- chi router
- pgx PostgreSQL driver
- JWT auth
- Telegram Bot API via HTTP

## Quick Start — local Postgres

```bash
cp .env.example .env
docker compose up -d postgres
go mod tidy
go run ./cmd/api
```

## Quick Start — Supabase Postgres

Copy `.env.supabase.example`, paste your Supabase Session pooler `DATABASE_URL`, then run:

```bash
cp .env.supabase.example .env
go mod tidy
go run ./cmd/api
```

Health:

```bash
curl http://localhost:8080/health
```

Create first organization/admin:

```bash
curl -X POST http://localhost:8080/api/v1/setup \
  -H 'Content-Type: application/json' \
  -d '{"org_name":"Demo Company","admin_name":"Owner","email":"admin@example.com","password":"admin123456"}'
```

Login:

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"admin123456"}'
```

Use the returned token:

```bash
Authorization: Bearer <token>
```


## API Endpoint Documentation

The complete endpoint reference is available here:

- `docs/API_ENDPOINTS.md` — full route list, roles, query params, payload examples, QR token image usage, face-device webhook examples, payroll/bank/EWA APIs.
- `docs/HTTP_EXAMPLES.md` — quick curl examples for common workflows.

Important v5.4 QR endpoints:

```text
POST /api/v1/attendance/qr-tokens
POST /api/v1/attendance/clock
```

QR token creation now returns:

```json
{
  "qr_image_data_url": "data:image/png;base64,...",
  "qr_image_base64": "...",
  "qr_content": "token-value",
  "expires_at": null
}
```

For a permanent wall QR code, send:

```json
{
  "branch_id": "branch-uuid",
  "label": "Permanent Office QR",
  "no_expiry": true,
  "require_gps": true,
  "qr_size_px": 512
}
```

Frontend display:

```html
<img src="RESPONSE.qr_image_data_url" alt="Attendance QR" />
```

## Main Endpoints

### System and Auth

- `GET /health`
- `POST /api/v1/setup`
- `POST /api/v1/auth/login`
- `GET /api/v1/me`

### Organization, Employees, Branches

- `GET /api/v1/employees`
- `POST /api/v1/employees`
- `PATCH /api/v1/employees/{id}`
- `POST /api/v1/branches`
- `GET /api/v1/branches`
- `POST /api/v1/departments`
- `GET /api/v1/departments`

### Shifts and Schedule Builder

- `POST /api/v1/shifts`
- `GET /api/v1/shifts`
- `POST /api/v1/schedule-assignments`
- `GET /api/v1/schedule-assignments`

### Smart Attendance

- `POST /api/v1/attendance/clock`
- `POST /api/v1/attendance/face-scan`
- `POST /api/v1/attendance/qr-tokens`
- `GET /api/v1/attendance/today?date=2026-06-05`
- `GET /api/v1/attendance/sessions?from=2026-06-01&to=2026-06-30&user_id=<uuid>`

QR clock example:

```json
{
  "kind": "in",
  "source": "qr",
  "qr_token": "token-from-admin",
  "lat": 11.5564,
  "lng": 104.9282,
  "gps_accuracy_m": 15
}
```

Face scan clock example:

```json
{
  "kind": "in",
  "source": "face_scan",
  "face_score": 91.5,
  "lat": 11.5564,
  "lng": 104.9282
}
```

### Leave and Overtime

- `POST /api/v1/leave/requests`
- `GET /api/v1/leave/requests`
- `PATCH /api/v1/leave/requests/{id}/approve`
- `POST /api/v1/overtime/requests`
- `GET /api/v1/overtime/requests`
- `PATCH /api/v1/overtime/requests/{id}/approve`

### CRM and Outside Sales

- `POST /api/v1/customers`
- `GET /api/v1/customers`
- `POST /api/v1/sales/visits/checkin`
- `PATCH /api/v1/sales/visits/{id}/checkout`
- `GET /api/v1/sales/visits?from=2026-06-01&to=2026-06-30`
- `GET /api/v1/sales/summary?date=2026-06-05`
- `POST /api/v1/sales/summary/telegram?date=2026-06-05`
- `POST /api/v1/kpis`
- `GET /api/v1/kpis?month=2026-06`

### Payroll and Payslips

- `GET /api/v1/payroll/rules`
- `PUT /api/v1/payroll/rules`
- `POST /api/v1/payroll/runs`
- `GET /api/v1/payroll/runs`
- `GET /api/v1/payroll/runs/{id}`
- `POST /api/v1/payroll/runs/{id}/approve`
- `POST /api/v1/payroll/runs/{id}/payout-export`
- `GET /api/v1/payroll/runs/{id}/export.csv`
- `GET /api/v1/payroll/runs/{id}/bank-statement.csv`
- `GET /api/v1/payroll/runs/{id}/payslips/{user_id}`

### Bank Transfer Batches

- `POST /api/v1/bank/accounts`
- `GET /api/v1/bank/accounts`
- `POST /api/v1/payroll/runs/{id}/bank-batches`
- `GET /api/v1/bank/batches`
- `POST /api/v1/bank/batches/{id}/mark-submitted`

### Earned Wage Access

- `POST /api/v1/ewa/requests`
- `GET /api/v1/ewa/requests`
- `PATCH /api/v1/ewa/requests/{id}/approve`

### Face Device Webhook

- `POST /api/v1/devices/face-events`
  - Header: `X-Device-Secret: <DEVICE_WEBHOOK_SECRET>`

### Reports

- `GET /api/v1/reports/summary?period=daily&date=2026-06-05`
- `GET /api/v1/reports/insights?period=monthly&date=2026-06-01`
- `POST /api/v1/reports/telegram-daily?date=2026-06-05`

## Roles

- `owner` — full access
- `admin` — HR/payroll/reports/admin operations
- `manager` — team attendance, leave, overtime, sales/KPI
- `sales` — attendance, customer visits
- `employee` — own attendance, leave, overtime, EWA, payslip

## Production Notes

1. Put the API behind HTTPS.
2. Replace `JWT_SECRET` and `DEVICE_WEBHOOK_SECRET` with long random secrets.
3. Use a proper migration tool in CI/CD.
4. Do not store raw biometric face templates in this API. Keep templates in the device vendor system or a secure encrypted biometric vault. This starter accepts face events and confidence scores only.
5. Keep `BANK_PROVIDER=manual_csv` until your bank provides an approved API specification, credentials, callback signing method, and test environment.
6. Run `go mod tidy` and `go test ./...` on a machine with internet access to download Go modules.

## v3 update notes

This package includes stability, security, and performance fixes:

- Fixed payroll generation to avoid pgx "connection busy" errors by closing the employee cursor before per-employee calculations.
- Prevented recalculating approved/paid payroll runs; only draft runs can be replaced.
- Added request panic recovery and security headers.
- Added 1 MB JSON body limit and strict single-object JSON decoding.
- Added employee search/pagination with `q`, `limit`, and `offset`.
- Limited normal employees/sales users to their own attendance records and employee profile data.
- Restricted CRM/sales endpoints to owner/admin/manager/sales roles.
- Required real `face_score` for `/attendance/face-scan`.
- Added GPS latitude/longitude validation and GPS accuracy validation.
- Added QR token TTL/radius validation and active branch check.
- Added shift time/date validation for schedule builder.
- Improved face-device webhook behavior for duplicate clock-ins and missing clock-out sessions.
- Counted pending + approved EWA requests when calculating available EWA balance.
- Added database pool tuning env variables.
- Added stronger database constraints and report indexes.

For production:

```bash
go mod tidy
go test ./...
go build ./cmd/api
```

The sandbox used to generate this ZIP cannot download external Go modules, so final dependency download must run on your machine/server.


## v4 update: Attendance Anti-Fraud V2 + Performance V2

### Attendance Anti-Fraud V2

The API now scores every mobile/GPS/QR/face-scan attendance event before saving it. It detects:

- Mock/fake GPS reported by the app.
- Poor GPS accuracy.
- Duplicate clock-in/clock-out attempts.
- QR token replay.
- Borderline face scores.
- Missing GPS evidence.
- Impossible travel speed between two GPS attendance points.

Manager APIs:

```text
GET   /api/v1/attendance/fraud-alerts
PATCH /api/v1/attendance/fraud-alerts/{id}/review
```

### Performance V2

Added:

- Request timeout middleware.
- Slow request logging.
- Bounded async worker queue for Telegram/report jobs.
- In-memory TTL cache for report/dashboard endpoints.
- Single-query report summary.
- Pagination for large list APIs.
- PostgreSQL indexes for attendance, fraud, dashboard, and sales reports.
- Performance stats endpoint:

```text
GET /api/v1/system/performance
```

### New environment variables

```env
REQUEST_TIMEOUT_SECONDS=15
SLOW_REQUEST_MS=700
CACHE_TTL_SECONDS=60
ASYNC_WORKER_LIMIT=8
FRAUD_WARN_SCORE=40
FRAUD_BLOCK_SCORE=100
FRAUD_MAX_SPEED_KPH=180
FRAUD_MAX_GPS_ACCURACY_M=80
FRAUD_DUPLICATE_SECONDS=120
```

See `docs/ANTI_FRAUD_V2.md` and `docs/PERFORMANCE_V2.md` for details.


## Existing Supabase tables

If your Supabase `public` schema already contains tables like `users`, `hosted_sites`, or bot tables, keep `DB_SCHEMA=checkinme`. The API will create and use the separate `checkinme` schema, so the migration will not conflict with your existing public tables.


## v5.3 optimization update

- Built-in API rate limiting for Render/Supabase deployments.
- Device webhook alias: `POST /api/v1/device/face-webhook`.
- Device webhooks support `employee_code`, `external_event_id`, and both device secret headers.
- Migration advisory lock to avoid concurrent startup migration races.
- Dockerfile hardened with non-root runtime user and smaller Go binary.

See `docs/V5_3_OPTIMIZATION.md` for details.
