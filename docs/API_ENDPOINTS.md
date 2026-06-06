# CheckinMe API Endpoints Reference

Version: v5.5 documentation update, based on v5.4 API routes.

Base URL examples:

- Local: `http://localhost:8080`
- Render: `https://YOUR_RENDER_URL.onrender.com`

All protected API endpoints use:

```http
Authorization: Bearer <JWT_TOKEN>
Content-Type: application/json
```

Public endpoints do not need a token. Device webhook endpoints use a device secret header instead of JWT.

## Roles

| Role | Meaning |
|---|---|
| `owner` | Full company owner access. |
| `admin` | HR, payroll, reports, employee management. |
| `manager` | Team attendance, leave, overtime, sales/KPI review. |
| `sales` | Attendance, customer records assigned to them, sales visits. |
| `employee` | Own attendance, leave, overtime, EWA, payslip, own bank account. |

## Standard response shape

Success responses usually return:

```json
{
  "ok": true
}
```

Error responses usually return:

```json
{
  "ok": false,
  "error": "message"
}
```

List endpoints that support pagination use:

```text
?limit=100&offset=0
```

## System

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `GET` | `/health` | Public | Health check with database ping. |
| `GET` | `/ready` | Public | Same as health; useful for Render readiness checks. |
| `GET` | `/api/v1/system/performance` | Owner/Admin | Runtime performance, cache, async worker, and PostgreSQL pool stats. |

### Health example

```bash
curl https://YOUR_RENDER_URL.onrender.com/health
```

Expected:

```json
{"ok":true,"database":true}
```

## Setup and Auth

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/setup` | Public, once only | Create first organization and owner admin. |
| `POST` | `/api/v1/auth/login` | Public | Login and return JWT token. |
| `GET` | `/api/v1/me` | Any logged-in user | Return JWT user claims. |

### Setup first owner

```bash
curl -X POST https://YOUR_RENDER_URL.onrender.com/api/v1/setup \
  -H "Content-Type: application/json" \
  -d '{
    "org_name":"Demo Company",
    "admin_name":"Owner",
    "email":"admin@example.com",
    "password":"admin123456"
  }'
```

### Login

```bash
curl -X POST https://YOUR_RENDER_URL.onrender.com/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","password":"admin123456"}'
```

## Employees

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/employees?q=&limit=&offset=` | Any logged-in user | List employees. Normal employees see limited data. |
| `POST` | `/api/v1/employees` | Owner/Admin/Manager | Create employee. |
| `PATCH` | `/api/v1/employees/{id}` | Owner/Admin/Manager | Update employee. |

### Create employee

```json
{
  "full_name": "Dara Sok",
  "email": "dara@example.com",
  "phone": "+85512345678",
  "password": "password123",
  "role": "employee",
  "employee_code": "EMP001",
  "branch_id": "optional-branch-uuid",
  "department_id": "optional-department-uuid",
  "manager_id": "optional-manager-uuid",
  "base_salary_cents": 35000,
  "currency": "USD"
}
```

Allowed roles: `owner`, `admin`, `manager`, `sales`, `employee`.

### Update employee

```json
{
  "full_name": "Dara Sok",
  "phone": "+85512345678",
  "role": "sales",
  "employee_code": "SALE001",
  "base_salary_cents": 40000,
  "currency": "USD",
  "active": true
}
```

## Branches

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/branches` | Any logged-in user | List branches. |
| `POST` | `/api/v1/branches` | Owner/Admin | Create branch and geofence radius. |

### Create branch

```json
{
  "name": "Phnom Penh HQ",
  "address": "Phnom Penh, Cambodia",
  "lat": 11.5564,
  "lng": 104.9282,
  "gps_radius_m": 150
}
```

`lat` and `lng` are optional, but if one is provided both must be valid.

## Departments

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/departments` | Any logged-in user | List departments. |
| `POST` | `/api/v1/departments` | Owner/Admin/Manager | Create department. |

### Create department

```json
{
  "name": "Sales Team",
  "description": "Outdoor sales department"
}
```

## Shifts and Schedule Builder

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/shifts` | Any logged-in user | List shift templates. |
| `POST` | `/api/v1/shifts` | Owner/Admin/Manager | Create shift template. |
| `GET` | `/api/v1/schedule-assignments` | Any logged-in user | List user/department shift assignments. |
| `POST` | `/api/v1/schedule-assignments` | Owner/Admin/Manager | Assign shift to user or department. |

### Create shift

```json
{
  "name": "Morning Shift",
  "start_time": "08:00",
  "end_time": "17:00",
  "break_minutes": 60,
  "grace_minutes": 5
}
```

Time format is `HH:MM` using 24-hour time.

### Create schedule assignment

```json
{
  "user_id": "optional-user-uuid",
  "department_id": "optional-department-uuid",
  "shift_id": "shift-uuid",
  "start_date": "2026-06-01",
  "end_date": "2026-06-30",
  "day_of_week": 1
}
```

Provide either `user_id` or `department_id`. `day_of_week` is optional: `0=Sunday`, `1=Monday`, ... `6=Saturday`.

## Smart Attendance

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/attendance/clock` | Any logged-in user | Clock in/out by GPS, QR, or device/mobile source. |
| `POST` | `/api/v1/attendance/face-scan` | Any logged-in user | Clock in/out with `face_score` required. |
| `POST` | `/api/v1/attendance/qr-tokens` | Owner/Admin/Manager | Generate QR token and scan-ready PNG image. |
| `GET` | `/api/v1/attendance/today?date=YYYY-MM-DD` | Any logged-in user | Today attendance summary. |
| `GET` | `/api/v1/attendance/sessions?from=YYYY-MM-DD&to=YYYY-MM-DD&user_id=` | Any logged-in user | Attendance sessions between dates. |

### Clock in with GPS

```json
{
  "kind": "in",
  "source": "gps",
  "lat": 11.5564,
  "lng": 104.9282,
  "gps_accuracy_m": 12,
  "mock_location": false,
  "device_id": "iphone-001",
  "note": "Normal clock in"
}
```

### Clock out

```json
{
  "kind": "out",
  "source": "gps",
  "lat": 11.5565,
  "lng": 104.9283,
  "gps_accuracy_m": 10,
  "mock_location": false,
  "device_id": "iphone-001"
}
```

### Face scan clock-in

```json
{
  "kind": "in",
  "source": "face_scan",
  "face_score": 96.5,
  "lat": 11.5564,
  "lng": 104.9282,
  "gps_accuracy_m": 20,
  "device_id": "face-mobile-001"
}
```

### Generate QR token with image

```json
{
  "branch_id": "branch-uuid",
  "label": "Front Desk QR",
  "ttl_minutes": 480,
  "require_gps": true,
  "allowed_radius_m": 150,
  "qr_size_px": 512
}
```

Response includes:

```json
{
  "ok": true,
  "id": "qr-token-id",
  "token": "secure-token-value",
  "expires_at": "2026-06-06T18:00:00Z",
  "qr_content": "secure-token-value",
  "qr_image_data_url": "data:image/png;base64,...",
  "qr_image_base64": "..."
}
```

Frontend usage:

```html
<img src="RESPONSE.qr_image_data_url" alt="Attendance QR" />
```

### Generate permanent/no-expiry QR token

Owner/admin/manager can create no-expiry QR tokens for wall posters:

```json
{
  "branch_id": "branch-uuid",
  "label": "Permanent Office QR",
  "no_expiry": true,
  "require_gps": true,
  "allowed_radius_m": 150,
  "qr_size_px": 512
}
```

Also accepted as no-expiry:

```json
{"unlimited": true}
```

or:

```json
{"ttl_minutes": 0}
```

### Generate QR with exact expiry time

```json
{
  "branch_id": "branch-uuid",
  "label": "Today Only QR",
  "expires_at": "2026-06-06T18:00:00+07:00",
  "require_gps": true,
  "qr_size_px": 512
}
```

### Clock in by QR scan

Use the scanned token value as `qr_token`:

```json
{
  "kind": "in",
  "source": "qr",
  "qr_token": "token-from-qr-scan",
  "lat": 11.5564,
  "lng": 104.9282,
  "gps_accuracy_m": 15,
  "mock_location": false,
  "device_id": "android-001"
}
```

## Attendance Anti-Fraud V2

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/attendance/fraud-alerts?status=&user_id=&limit=&offset=` | Owner/Admin/Manager | List suspicious attendance events. |
| `PATCH` | `/api/v1/attendance/fraud-alerts/{id}/review` | Owner/Admin/Manager | Mark fraud alert as reviewed/false positive/confirmed. |

Fraud statuses:

- `normal`
- `warning`
- `needs_review`
- `blocked`
- `reviewed`
- `false_positive`
- `confirmed`

### Review fraud alert

```json
{
  "status": "confirmed",
  "note": "Manager confirmed fake GPS evidence."
}
```

Allowed review statuses: `reviewed`, `false_positive`, `confirmed`.

## Leave Requests

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/leave/requests` | Any logged-in user | Submit leave request. |
| `GET` | `/api/v1/leave/requests?status=` | Any logged-in user | List leave requests. Employees see their own only. |
| `PATCH` | `/api/v1/leave/requests/{id}/approve` | Owner/Admin/Manager | Approve or reject leave request. |

### Create leave request

```json
{
  "leave_type": "annual",
  "start_date": "2026-06-10",
  "end_date": "2026-06-12",
  "reason": "Family trip"
}
```

### Review leave request

```json
{
  "status": "approved",
  "note": "Approved by manager"
}
```

## Overtime Requests

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/overtime/requests` | Any logged-in user | Submit overtime request. |
| `GET` | `/api/v1/overtime/requests?status=` | Any logged-in user | List overtime requests. Employees see their own only. |
| `PATCH` | `/api/v1/overtime/requests/{id}/approve` | Owner/Admin/Manager | Approve or reject overtime request. |

### Create overtime request

```json
{
  "work_date": "2026-06-10",
  "minutes": 120,
  "reason": "Monthly stock count"
}
```

### Review overtime request

```json
{
  "status": "approved",
  "note": "Approved"
}
```

## CRM and Outside Sales Tracker

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/customers?limit=&offset=` | Owner/Admin/Manager/Sales | List customers. Sales users see assigned customers. |
| `POST` | `/api/v1/customers` | Owner/Admin/Manager/Sales | Create customer. |
| `POST` | `/api/v1/sales/visits/checkin` | Owner/Admin/Manager/Sales | GPS customer visit check-in. |
| `PATCH` | `/api/v1/sales/visits/{id}/checkout` | Owner/Admin/Manager/Sales | Sales visit checkout. |
| `GET` | `/api/v1/sales/visits?from=&to=&user_id=&limit=&offset=` | Owner/Admin/Manager/Sales | List sales visits. Sales users see their own data. |
| `GET` | `/api/v1/sales/summary?date=YYYY-MM-DD` | Any logged-in user | Daily sales route summary. |
| `POST` | `/api/v1/sales/summary/telegram?date=YYYY-MM-DD` | Owner/Admin/Manager | Send sales summary to Telegram. |

### Create customer

```json
{
  "name": "ABC Mini Mart",
  "phone": "+85512345678",
  "address": "Phnom Penh",
  "assigned_to": "sales-user-uuid",
  "lat": 11.5564,
  "lng": 104.9282
}
```

### Sales visit check-in

```json
{
  "customer_id": "customer-uuid",
  "lat": 11.5564,
  "lng": 104.9282,
  "notes": "Arrived at client shop"
}
```

### Sales visit checkout

```json
{
  "notes": "Order collected and follow-up scheduled"
}
```

## KPI Management

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/kpis` | Owner/Admin/Manager | Create/update monthly KPI target. |
| `GET` | `/api/v1/kpis?month=YYYY-MM` | Any logged-in user | List KPI progress. Sales users see their own. |

### Upsert KPI

```json
{
  "user_id": "sales-user-uuid",
  "month": "2026-06",
  "visits_target": 120,
  "sales_target_cents": 500000
}
```

## Payroll and Payslips

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/payroll/rules` | Owner/Admin | List payroll rules. |
| `PUT` | `/api/v1/payroll/rules` | Owner/Admin | Update payroll rules. |
| `POST` | `/api/v1/payroll/runs` | Owner/Admin | Generate payroll run. |
| `GET` | `/api/v1/payroll/runs` | Owner/Admin | List payroll runs. |
| `GET` | `/api/v1/payroll/runs/{id}` | Owner/Admin | Get payroll run with items. |
| `POST` | `/api/v1/payroll/runs/{id}/approve` | Owner/Admin | Approve payroll run. |
| `POST` | `/api/v1/payroll/runs/{id}/payout-export` | Owner/Admin | Mark or prepare payout export. |
| `GET` | `/api/v1/payroll/runs/{id}/export.csv` | Owner/Admin | Download payroll CSV. |
| `GET` | `/api/v1/payroll/runs/{id}/bank-statement.csv` | Owner/Admin | Download bank statement CSV. |
| `GET` | `/api/v1/payroll/runs/{id}/payslips/{user_id}` | Owner/Admin or matching user | Get digital payslip. |

### Update payroll rules

Use numeric values as percentages or cents depending on the rule. Example:

```json
{
  "rules": {
    "late_deduction_per_minute_cents": 5,
    "overtime_multiplier": 1.5,
    "ewa_max_percent_of_monthly_salary": 0.4,
    "nssf_employee_percent": 0.0,
    "nssf_employer_percent": 0.0
  }
}
```

### Create payroll run

```json
{
  "month": "2026-06"
}
```

### Approve payroll run

```json
{}
```

## Bank Accounts and Transfer Batches

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/bank/accounts` | Any logged-in user | List bank accounts. Employees see their own. |
| `POST` | `/api/v1/bank/accounts` | Any logged-in user | Create/update bank account. Managers/admins can create for employees. |
| `POST` | `/api/v1/payroll/runs/{id}/bank-batches` | Owner/Admin | Create transfer batch from payroll run. |
| `GET` | `/api/v1/bank/batches` | Owner/Admin | List bank transfer batches. |
| `POST` | `/api/v1/bank/batches/{id}/mark-submitted` | Owner/Admin | Mark draft batch as submitted. |

### Create bank account

```json
{
  "user_id": "employee-user-uuid",
  "bank_name": "PPCBank",
  "account_name": "Dara Sok",
  "account_number": "00123456789",
  "currency": "USD",
  "is_primary": true
}
```

### Create bank batch

```json
{
  "provider": "manual_csv"
}
```

## Earned Wage Access \(EWA\)

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/ewa/requests` | Any logged-in user | Request early withdrawal from earned salary. |
| `GET` | `/api/v1/ewa/requests` | Any logged-in user | List EWA requests. Employees see their own. |
| `PATCH` | `/api/v1/ewa/requests/{id}/approve` | Owner/Admin/Manager | Approve or reject EWA request. |

### Create EWA request

```json
{
  "amount_cents": 5000,
  "reason": "Emergency expense"
}
```

### Review EWA request

```json
{
  "status": "approved",
  "note": "Approved for payout"
}
```

## Reports and Analytics

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/reports/summary?period=daily&date=YYYY-MM-DD` | Any logged-in user | Summary report with cache. |
| `GET` | `/api/v1/reports/insights?period=monthly&date=YYYY-MM-DD` | Any logged-in user | Simple analytics insights. |
| `POST` | `/api/v1/reports/telegram-daily?date=YYYY-MM-DD` | Owner/Admin/Manager | Send daily Telegram report. |

Allowed `period` values: `daily`, `weekly`, `monthly`, `yearly`.

## Face Device Webhook

These endpoints are for external face attendance devices. They do not use JWT. They require the secret header.

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/devices/face-events` | Device secret | Face device event. |
| `POST` | `/api/v1/device/face-webhook` | Device secret | Alias for face device event. |

Allowed headers:

```http
X-Device-Webhook-Secret: <DEVICE_WEBHOOK_SECRET>
```

or:

```http
X-Device-Secret: <DEVICE_WEBHOOK_SECRET>
```

### Face device event by employee code

```json
{
  "employee_code": "EMP001",
  "kind": "in",
  "device_id": "face-device-001",
  "external_event_id": "vendor-event-0001",
  "face_score": 96.0,
  "timestamp": "2026-06-06T09:00:00+07:00"
}
```

### Face device event by user ID

```json
{
  "user_id": "employee-user-uuid",
  "kind": "out",
  "device_id": "face-device-001",
  "external_event_id": "vendor-event-0002",
  "face_score": 97.0,
  "timestamp": "2026-06-06T18:00:00+07:00"
}
```

`external_event_id` is recommended for idempotency so duplicate device retries do not create duplicate punches.

## Important production notes

1. Use HTTPS only.
2. Keep `JWT_SECRET`, `DEVICE_WEBHOOK_SECRET`, `DATABASE_URL`, and Telegram tokens only in Render/Supabase env variables.
3. Use `DB_SCHEMA=checkinme` on Supabase to avoid conflict with existing `public.users` tables.
4. Keep `AUTO_MIGRATE=true` for first deploy; after production stabilizes, consider controlled migrations in CI/CD.
5. Keep bank payout in `manual_csv` mode until the bank gives official API specs, credentials, signing, and sandbox/test environment.
6. This API stores face scores/events only; do not store raw biometric templates in this system unless you add encrypted biometric vault controls.

### QR Token 500 troubleshooting

If `POST /api/v1/attendance/qr-tokens` returns HTTP 500 after upgrading from an older version:

1. Confirm Render env has `AUTO_MIGRATE=true` and `DB_SCHEMA=checkinme`.
2. Redeploy the latest code.
3. If it still fails, run `docs/SUPABASE_SQL_EDITOR_V5_6_QR_500_FIX.sql` in Supabase SQL Editor.
4. Confirm your request uses a real branch UUID from `GET /api/v1/branches`.

Example branch lookup:

```bash
curl https://YOUR_RENDER_URL.onrender.com/api/v1/branches \
  -H "Authorization: Bearer $TOKEN"
```

Example QR request:

```bash
curl -X POST https://YOUR_RENDER_URL.onrender.com/api/v1/attendance/qr-tokens \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "branch_id":"PASTE_REAL_BRANCH_UUID_HERE",
    "label":"Main Office QR",
    "no_expiry":true,
    "require_gps":true,
    "qr_size_px":512
  }'
```
