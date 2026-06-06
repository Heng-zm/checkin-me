# Deploy CheckinMe Go API with Supabase Postgres

This version uses Supabase as the PostgreSQL database. The Go API still talks to Postgres directly through `DATABASE_URL`; it does not require Supabase Auth or Supabase Storage.

## 1. Create Supabase project

1. Open Supabase Dashboard.
2. Create a new project.
3. Save your database password.
4. Go to **Connect**.
5. Copy the **Session pooler** connection string if you deploy on Render.

Recommended connection for Render:

```env
DATABASE_URL=postgresql://postgres.PROJECT_REF:YOUR_DB_PASSWORD@aws-0-ap-southeast-1.pooler.supabase.com:5432/postgres?sslmode=require
DB_QUERY_EXEC_MODE=auto
DB_SCHEMA=checkinme
```

Why Session pooler? Supabase direct connections are IPv6 by default unless you enable the IPv4 add-on. The Supavisor pooler is the safer choice for platforms that may not support direct IPv6 database connections.

## 2. If using Transaction pooler

Transaction pooler strings usually use port `6543`.

Use this only if you need it:

```env
DATABASE_URL=postgresql://postgres.PROJECT_REF:YOUR_DB_PASSWORD@aws-0-ap-southeast-1.pooler.supabase.com:6543/postgres?sslmode=require
DB_QUERY_EXEC_MODE=simple_protocol
```

`simple_protocol` avoids prepared-statement problems that can happen with transaction poolers.

## 3. Deploy on Render using Supabase

This package includes a `render.yaml` that creates only the API web service. It does not create Render Postgres.

Steps:

1. Push this project to GitHub.
2. In Render, create a new Blueprint from the repo.
3. Open the created web service.
4. Go to **Environment**.
5. Set `DATABASE_URL` to your Supabase Session pooler connection string.
6. Deploy or redeploy the service.

Render environment variables should hold secrets like database URLs and API keys. Do not commit real Supabase credentials to GitHub.

## 4. Required Render env vars

```env
APP_ENV=production
PORT=10000
DATABASE_URL=postgresql://postgres.PROJECT_REF:YOUR_DB_PASSWORD@aws-0-ap-southeast-1.pooler.supabase.com:5432/postgres?sslmode=require
JWT_SECRET=your-long-random-secret
DEVICE_WEBHOOK_SECRET=your-long-random-webhook-secret
DEFAULT_TIMEZONE=Asia/Phnom_Penh
AUTO_MIGRATE=true
DB_MAX_CONNS=8
DB_MIN_CONNS=1
DB_MAX_CONN_IDLE_MINUTES=5
DB_QUERY_EXEC_MODE=auto
DB_SCHEMA=checkinme
```

## 5. First production test

```bash
curl https://YOUR_RENDER_URL.onrender.com/health
```

Expected:

```json
{"ok":true,"database":true}
```

Create the first admin:

```bash
curl -X POST https://YOUR_RENDER_URL.onrender.com/api/v1/setup \
  -H "Content-Type: application/json" \
  -d '{"org_name":"Demo Company","admin_name":"Owner","email":"admin@example.com","password":"admin123456"}'
```

## 6. Supabase table check

After the first startup with `AUTO_MIGRATE=true`, open Supabase SQL editor and run:

```sql
select table_name
from information_schema.tables
where table_schema = 'public'
order by table_name;
```

You should see CheckinMe tables such as `organizations`, `users`, `attendance_events`, `attendance_sessions`, `payroll_runs`, and `sales_visits`.

## 7. Important production notes

- Keep `DB_MAX_CONNS` conservative on small Supabase projects.
- Keep `AUTO_MIGRATE=true` for first deploy, then you may set it to `false` after migrations are applied.
- Do not paste Supabase service role keys into this API unless you later add Supabase Storage/Auth features.
- Do not expose database credentials in frontend apps.


## Existing Supabase tables

If your Supabase `public` schema already contains tables like `users`, `hosted_sites`, or bot tables, keep `DB_SCHEMA=checkinme`. The API will create and use the separate `checkinme` schema, so the migration will not conflict with your existing public tables.
