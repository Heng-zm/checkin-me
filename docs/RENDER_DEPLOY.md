# Deploy CheckinMe API to Render with Supabase

This project is ready for Render using `render.yaml` and Docker. In this Supabase build, Render deploys only the Go API. Your database is Supabase Postgres.

## 1. Create Supabase database

1. Create a Supabase project.
2. Open **Connect** in Supabase.
3. Copy the **Session pooler** connection string, usually port `5432`.
4. Add `?sslmode=require` if it is not already present.

Recommended:

```env
DATABASE_URL=postgresql://postgres.PROJECT_REF:YOUR_DB_PASSWORD@aws-0-ap-southeast-1.pooler.supabase.com:5432/postgres?sslmode=require
DB_QUERY_EXEC_MODE=auto
```

If you use Transaction pooler on port `6543`, set:

```env
DB_QUERY_EXEC_MODE=simple_protocol
```

## 2. Push to GitHub

```bash
git init
git add .
git commit -m "Deploy CheckinMe API to Render with Supabase"
git branch -M main
git remote add origin https://github.com/YOUR_USERNAME/checkinme-go-api.git
git push -u origin main
```

## 3. Create Render Blueprint

1. Open Render Dashboard.
2. New > Blueprint.
3. Connect the GitHub repository.
4. Render reads `render.yaml` and creates `checkinme-api` web service.
5. Open the web service > Environment.
6. Paste your Supabase `DATABASE_URL`.
7. Deploy again if Render does not automatically redeploy.

The API listens on `$PORT`. This Blueprint sets `PORT=10000`.

## 4. Environment variables

Render generates:

- `JWT_SECRET`
- `DEVICE_WEBHOOK_SECRET`

Set manually:

- `DATABASE_URL` from Supabase Session pooler
- `TELEGRAM_BOT_TOKEN`, optional
- `TELEGRAM_DEFAULT_CHAT_ID`, optional
- `BANK_API_BASE_URL`, optional
- `BANK_API_KEY`, optional

Keep `BANK_PROVIDER=manual_csv` until your bank provides official test credentials and API docs.

## 5. Migrations

`AUTO_MIGRATE=true` runs the embedded SQL migration on startup. The migration is idempotent and records applied files in `schema_migrations`.

After first successful deploy, you may set `AUTO_MIGRATE=false` if you prefer controlled/manual migration.

## 6. Test deployed API

Replace `YOUR_RENDER_URL` with the Render service URL.

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

Login:

```bash
curl -X POST https://YOUR_RENDER_URL.onrender.com/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","password":"admin123456"}'
```

## 7. Production notes

- Replace `CORS_ALLOWED_ORIGINS=*` with your real frontend URL before production.
- Keep database credentials in Render environment variables only.
- Keep `DB_MAX_CONNS` conservative on small Supabase plans.
- Do not store bank credentials in code.
- Do not expose `DATABASE_URL` in frontend apps.
- Review logs after first deploy. If health check fails, check `DATABASE_URL`, SSL mode, and Supabase pooler mode.
