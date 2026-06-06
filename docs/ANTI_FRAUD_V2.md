# Attendance Anti-Fraud V2

The attendance anti-fraud engine runs before a clock event is saved.

## Fraud signals

| Signal | Example | Default score |
|---|---:|---:|
| Mock/fake GPS reported by app | `mock_location=true` | 100 |
| Bad GPS accuracy | `gps_accuracy_m > FRAUD_MAX_GPS_ACCURACY_M` | 25 |
| GPS/QR clock missing location | no `lat/lng` for GPS/QR evidence | 45 |
| Mobile clock without GPS | no `lat/lng` | 15 |
| Borderline face score | face score between 70 and 80 | 30 |
| QR replay | same employee reuses QR quickly | 35 |
| Duplicate clock action | same kind within duplicate window | 25 |
| Suspicious travel | speed above max km/h | 60 |
| Impossible travel | speed above 2x max km/h | 100 |

## Default thresholds

```env
FRAUD_WARN_SCORE=40
FRAUD_BLOCK_SCORE=100
FRAUD_MAX_SPEED_KPH=180
FRAUD_MAX_GPS_ACCURACY_M=80
FRAUD_DUPLICATE_SECONDS=120
```

## Status behavior

- `normal`: saved without warning.
- `warning`: saved, low-risk signal shown.
- `needs_review`: saved and visible in fraud alerts.
- `blocked`: attendance is rejected and written to `audit_logs` only.
- `reviewed`, `false_positive`, `confirmed`: manager review outcomes.

## API examples

List fraud alerts:

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/attendance/fraud-alerts?from=2026-06-01&to=2026-06-30&status=needs_review"
```

Review an alert:

```bash
curl -X PATCH "http://localhost:8080/api/v1/attendance/fraud-alerts/$EVENT_ID/review" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"status":"confirmed","note":"Impossible travel confirmed by manager"}'
```
