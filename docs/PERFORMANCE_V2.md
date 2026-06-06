# Performance V2

## What changed

- Request timeout middleware protects the server from long-running requests.
- Slow request logging helps find expensive endpoints.
- Report summary uses one SQL query instead of five separate queries.
- Memory TTL cache speeds up dashboard/report summary calls.
- Bounded async worker queue prevents unlimited Telegram goroutines.
- Pagination was added to large list APIs.
- Extra indexes were added for attendance, fraud, sales, and dashboard reports.

## Environment variables

```env
REQUEST_TIMEOUT_SECONDS=15
SLOW_REQUEST_MS=700
CACHE_TTL_SECONDS=60
ASYNC_WORKER_LIMIT=8
DB_MAX_CONNS=20
DB_MIN_CONNS=2
DB_MAX_CONN_IDLE_MINUTES=10
```

## Performance endpoint

```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/system/performance
```

This returns PostgreSQL pool stats, cache item count, and async worker usage.

## Production recommendation

For one API container, the in-memory cache is enough. For multiple API containers, replace `internal/cache` with Redis or keep a short TTL such as 15–60 seconds.
