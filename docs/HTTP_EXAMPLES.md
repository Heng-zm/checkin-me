# CheckinMe API Examples

Replace `$TOKEN` with the JWT from login.

## Create employee

```bash
curl -X POST http://localhost:8080/api/v1/employees \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"full_name":"Dara Sok","email":"dara@example.com","password":"password123","role":"sales","base_salary_cents":35000,"currency":"USD"}'
```

## Clock in with GPS

```bash
curl -X POST http://localhost:8080/api/v1/attendance/clock \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"kind":"in","lat":11.5564,"lng":104.9282,"gps_accuracy_m":12,"note":"Office"}'
```

## Request leave

```bash
curl -X POST http://localhost:8080/api/v1/leave/requests \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"leave_type":"annual","start_date":"2026-06-10","end_date":"2026-06-11","reason":"Family"}'
```

## Create customer and visit

```bash
curl -X POST http://localhost:8080/api/v1/customers \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Client A","phone":"012345678","address":"Phnom Penh","lat":11.5564,"lng":104.9282}'
```

```bash
curl -X POST http://localhost:8080/api/v1/sales/visits/checkin \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"customer_id":"<customer-id>","lat":11.5565,"lng":104.9281,"notes":"Meeting started"}'
```

## Payroll run

```bash
curl -X POST http://localhost:8080/api/v1/payroll/runs \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"month":"2026-06"}'
```

## Face device webhook

```bash
curl -X POST http://localhost:8080/api/v1/devices/face-events \
  -H 'X-Device-Secret: change-this-device-webhook-secret' \
  -H 'Content-Type: application/json' \
  -d '{"org_id":"<org-id>","user_id":"<user-id>","device_sn":"FACE-001","event_type":"in","face_score":98.5,"event_at":"2026-06-05T09:00:00+07:00"}'
```

## Extended Module Examples

### Create Department

```bash
curl -X POST http://localhost:8080/api/v1/departments \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Sales","description":"Outside sales team"}'
```

### Create Shift

```bash
curl -X POST http://localhost:8080/api/v1/shifts \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Morning","start_time":"08:00","end_time":"17:00","break_minutes":60,"grace_minutes":5}'
```

### Assign Shift to Department

```bash
curl -X POST http://localhost:8080/api/v1/schedule-assignments \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"department_id":"<department_uuid>","shift_id":"<shift_uuid>","start_date":"2026-06-01","end_date":"2026-12-31"}'
```

### Create Local QR Token for Branch

The response now includes `qr_image_data_url` and `qr_image_base64`, so your dashboard can show the QR image immediately after generation.

```bash
curl -X POST http://localhost:8080/api/v1/attendance/qr-tokens \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"branch_id":"<branch_uuid>","label":"Front desk QR","ttl_minutes":480,"require_gps":true,"qr_size_px":512}'
```

### Create No-Expiry QR Token for Wall Scan

Only `owner` and `admin` users can create permanent/no-expiry QR tokens.

```bash
curl -X POST http://localhost:8080/api/v1/attendance/qr-tokens \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"branch_id":"<branch_uuid>","label":"Permanent office QR","no_expiry":true,"require_gps":true,"qr_size_px":512}'
```

### Create QR Token with Exact Expiry Time

```bash
curl -X POST http://localhost:8080/api/v1/attendance/qr-tokens \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"branch_id":"<branch_uuid>","label":"Today only QR","expires_at":"2026-06-06T18:00:00+07:00","require_gps":true}'
```

### Clock In by QR + GPS

```bash
curl -X POST http://localhost:8080/api/v1/attendance/clock \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"kind":"in","qr_token":"<qr_token>","lat":11.5564,"lng":104.9282,"gps_accuracy_m":15}'
```

### Clock In by Face Scan

```bash
curl -X POST http://localhost:8080/api/v1/attendance/face-scan \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"kind":"in","source":"face_scan","face_score":92.4,"lat":11.5564,"lng":104.9282}'
```

### Sales Daily Summary

```bash
curl "http://localhost:8080/api/v1/sales/summary?date=2026-06-05" \
  -H "Authorization: Bearer $TOKEN"
```

### Add Employee Bank Account

```bash
curl -X POST http://localhost:8080/api/v1/bank/accounts \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"<employee_uuid>","bank_name":"PPCBank","account_name":"Employee Name","account_number":"123456789","currency":"USD","is_primary":true}'
```

### Payroll Exports

```bash
curl -OJ "http://localhost:8080/api/v1/payroll/runs/<run_uuid>/export.csv" \
  -H "Authorization: Bearer $TOKEN"

curl -OJ "http://localhost:8080/api/v1/payroll/runs/<run_uuid>/bank-statement.csv" \
  -H "Authorization: Bearer $TOKEN"
```

### Create Bank Transfer Batch

```bash
curl -X POST http://localhost:8080/api/v1/payroll/runs/<run_uuid>/bank-batches \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"provider":"manual_csv"}'
```

### Request Earned Wage Access

```bash
curl -X POST http://localhost:8080/api/v1/ewa/requests \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"amount_cents":5000,"reason":"Emergency family expense"}'
```

## Attendance Anti-Fraud V2

Clock in with app-side mock location detection:

```bash
curl -X POST http://localhost:8080/api/v1/attendance/clock \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "kind":"in",
    "source":"gps",
    "lat":11.5564,
    "lng":104.9282,
    "gps_accuracy_m":12,
    "mock_location":false,
    "device_id":"iphone-001"
  }'
```

List suspicious attendance alerts:

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/attendance/fraud-alerts?from=2026-06-01&to=2026-06-30&limit=50&offset=0"
```

Review fraud alert:

```bash
curl -X PATCH http://localhost:8080/api/v1/attendance/fraud-alerts/$EVENT_ID/review \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"status":"confirmed","note":"Manager verified suspicious GPS jump."}'
```

## Performance V2

Check server performance counters:

```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/system/performance
```
