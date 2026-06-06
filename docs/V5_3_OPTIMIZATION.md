# CheckinMe v5.3 Optimization Notes

## New environment variables

```env
RATE_LIMIT_ENABLED=true
RATE_LIMIT_REQUESTS_PER_MINUTE=240
RATE_LIMIT_BURST=80
```

The rate limiter is an in-memory token bucket keyed by client IP. It skips `OPTIONS`, `/health`, and `/ready`.

## Device webhook compatibility

Supported routes:

```text
POST /api/v1/devices/face-events
POST /api/v1/device/face-webhook
```

Supported secret headers:

```text
X-Device-Webhook-Secret: your_secret
X-Device-Secret: your_secret
```

Recommended payload:

```json
{
  "org_id": "ORG_UUID",
  "employee_code": "EMP001",
  "device_sn": "FACE-DEVICE-001",
  "external_event_id": "FACE-DEVICE-001-20260606090000-EMP001-IN",
  "event_type": "in",
  "event_at": "2026-06-06T09:00:00Z",
  "face_score": 96.5,
  "lat": 11.5564,
  "lng": 104.9282
}
```

Use `external_event_id` whenever your device supports it. If the device sends the same event again, the API returns `duplicate: true` instead of creating another attendance event.

## Migration safety

The app now uses PostgreSQL advisory locks before applying migrations. This prevents migration race conditions when Render starts more than one instance or restarts quickly.

## Docker hardening

The Docker runtime stage now runs as a non-root user and builds a smaller binary with `-trimpath -ldflags="-s -w"`.
