CREATE EXTENSION IF NOT EXISTS pgcrypto;

DO $$ BEGIN
    CREATE TYPE user_role AS ENUM ('owner', 'admin', 'manager', 'sales', 'employee');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
    CREATE TYPE attendance_kind AS ENUM ('in', 'out');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
    CREATE TYPE approval_status AS ENUM ('pending', 'approved', 'rejected', 'cancelled');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
    CREATE TYPE payroll_status AS ENUM ('draft', 'approved', 'paid', 'cancelled');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'Asia/Phnom_Penh',
    telegram_chat_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS branches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    address TEXT,
    lat NUMERIC(10,7),
    lng NUMERIC(10,7),
    gps_radius_m INT NOT NULL DEFAULT 150,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (gps_radius_m > 0),
    CHECK ((lat IS NULL AND lng IS NULL) OR (lat BETWEEN -90 AND 90 AND lng BETWEEN -180 AND 180))
);
CREATE INDEX IF NOT EXISTS idx_branches_org ON branches(org_id);

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    branch_id UUID REFERENCES branches(id) ON DELETE SET NULL,
    manager_id UUID REFERENCES users(id) ON DELETE SET NULL,
    full_name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    phone TEXT,
    password_hash TEXT NOT NULL,
    role user_role NOT NULL DEFAULT 'employee',
    base_salary_cents BIGINT NOT NULL DEFAULT 0,
    currency CHAR(3) NOT NULL DEFAULT 'USD',
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (base_salary_cents >= 0)
);
CREATE INDEX IF NOT EXISTS idx_users_org ON users(org_id);
CREATE INDEX IF NOT EXISTS idx_users_manager ON users(manager_id);

CREATE TABLE IF NOT EXISTS attendance_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind attendance_kind NOT NULL,
    event_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lat NUMERIC(10,7),
    lng NUMERIC(10,7),
    gps_accuracy_m INT,
    source TEXT NOT NULL DEFAULT 'mobile',
    device_sn TEXT,
    face_score NUMERIC(5,2),
    note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((lat IS NULL AND lng IS NULL) OR (lat BETWEEN -90 AND 90 AND lng BETWEEN -180 AND 180)),
    CHECK (gps_accuracy_m IS NULL OR gps_accuracy_m >= 0)
);
CREATE INDEX IF NOT EXISTS idx_attendance_org_time ON attendance_events(org_id, event_at DESC);
CREATE INDEX IF NOT EXISTS idx_attendance_user_time ON attendance_events(user_id, event_at DESC);

CREATE TABLE IF NOT EXISTS attendance_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    clock_in_id UUID NOT NULL REFERENCES attendance_events(id) ON DELETE CASCADE,
    clock_out_id UUID REFERENCES attendance_events(id) ON DELETE SET NULL,
    clock_in_at TIMESTAMPTZ NOT NULL,
    clock_out_at TIMESTAMPTZ,
    total_minutes INT NOT NULL DEFAULT 0,
    late_minutes INT NOT NULL DEFAULT 0,
    overtime_minutes INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_sessions_org_user ON attendance_sessions(org_id, user_id, clock_in_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_open ON attendance_sessions(user_id) WHERE clock_out_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_one_open_per_user ON attendance_sessions(org_id, user_id) WHERE clock_out_at IS NULL;

CREATE TABLE IF NOT EXISTS leave_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    leave_type TEXT NOT NULL,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    reason TEXT,
    status approval_status NOT NULL DEFAULT 'pending',
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    review_note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_leave_org_status ON leave_requests(org_id, status, start_date DESC);

CREATE TABLE IF NOT EXISTS overtime_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    work_date DATE NOT NULL,
    minutes INT NOT NULL CHECK (minutes > 0),
    reason TEXT,
    status approval_status NOT NULL DEFAULT 'pending',
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    review_note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_overtime_org_status ON overtime_requests(org_id, status, work_date DESC);

CREATE TABLE IF NOT EXISTS customers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    assigned_to UUID REFERENCES users(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    phone TEXT,
    address TEXT,
    lat NUMERIC(10,7),
    lng NUMERIC(10,7),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((lat IS NULL AND lng IS NULL) OR (lat BETWEEN -90 AND 90 AND lng BETWEEN -180 AND 180))
);
CREATE INDEX IF NOT EXISTS idx_customers_org ON customers(org_id);
CREATE INDEX IF NOT EXISTS idx_customers_assigned ON customers(assigned_to);

CREATE TABLE IF NOT EXISTS sales_visits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    check_in_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    check_out_at TIMESTAMPTZ,
    lat NUMERIC(10,7),
    lng NUMERIC(10,7),
    checkout_lat NUMERIC(10,7),
    checkout_lng NUMERIC(10,7),
    distance_m INT,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((lat IS NULL AND lng IS NULL) OR (lat BETWEEN -90 AND 90 AND lng BETWEEN -180 AND 180)),
    CHECK ((checkout_lat IS NULL AND checkout_lng IS NULL) OR (checkout_lat BETWEEN -90 AND 90 AND checkout_lng BETWEEN -180 AND 180)),
    CHECK (distance_m IS NULL OR distance_m >= 0)
);
CREATE INDEX IF NOT EXISTS idx_sales_visits_org_time ON sales_visits(org_id, check_in_at DESC);

CREATE TABLE IF NOT EXISTS kpis (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    month DATE NOT NULL,
    visits_target INT NOT NULL DEFAULT 0,
    sales_target_cents BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(org_id, user_id, month)
);

CREATE TABLE IF NOT EXISTS payroll_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    rule_key TEXT NOT NULL,
    rule_value NUMERIC(18,6) NOT NULL,
    description TEXT,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    UNIQUE(org_id, rule_key)
);

CREATE TABLE IF NOT EXISTS payroll_tax_brackets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    currency CHAR(3) NOT NULL DEFAULT 'KHR',
    min_amount BIGINT NOT NULL,
    max_amount BIGINT,
    rate NUMERIC(8,6) NOT NULL,
    deduction BIGINT NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    UNIQUE(org_id, currency, min_amount)
);

CREATE TABLE IF NOT EXISTS payroll_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    month DATE NOT NULL,
    status payroll_status NOT NULL DEFAULT 'draft',
    gross_cents BIGINT NOT NULL DEFAULT 0,
    deductions_cents BIGINT NOT NULL DEFAULT 0,
    employer_cost_cents BIGINT NOT NULL DEFAULT 0,
    net_cents BIGINT NOT NULL DEFAULT 0,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    approved_by UUID REFERENCES users(id) ON DELETE SET NULL,
    approved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(org_id, month)
);

CREATE TABLE IF NOT EXISTS payroll_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    payroll_run_id UUID NOT NULL REFERENCES payroll_runs(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    base_salary_cents BIGINT NOT NULL DEFAULT 0,
    overtime_cents BIGINT NOT NULL DEFAULT 0,
    bonus_cents BIGINT NOT NULL DEFAULT 0,
    late_deduction_cents BIGINT NOT NULL DEFAULT 0,
    unpaid_leave_deduction_cents BIGINT NOT NULL DEFAULT 0,
    tax_cents BIGINT NOT NULL DEFAULT 0,
    nssf_employee_cents BIGINT NOT NULL DEFAULT 0,
    nssf_employer_cents BIGINT NOT NULL DEFAULT 0,
    gross_cents BIGINT NOT NULL DEFAULT 0,
    net_cents BIGINT NOT NULL DEFAULT 0,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(payroll_run_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_payroll_items_run ON payroll_items(payroll_run_id);

CREATE TABLE IF NOT EXISTS device_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_sn TEXT NOT NULL,
    event_type TEXT NOT NULL,
    event_at TIMESTAMPTZ NOT NULL,
    face_score NUMERIC(5,2),
    raw_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    actor_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    entity TEXT,
    entity_id UUID,
    meta JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_org_time ON audit_logs(org_id, created_at DESC);

-- Extended CheckinMe modules: department schedule builder, QR attendance, bank batches, and EWA.
CREATE TABLE IF NOT EXISTS departments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(org_id, name)
);
CREATE INDEX IF NOT EXISTS idx_departments_org ON departments(org_id);

ALTER TABLE users ADD COLUMN IF NOT EXISTS department_id UUID REFERENCES departments(id) ON DELETE SET NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS employee_code TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_org_employee_code ON users(org_id, employee_code) WHERE employee_code IS NOT NULL;

CREATE TABLE IF NOT EXISTS shifts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    start_time TIME NOT NULL,
    end_time TIME NOT NULL,
    break_minutes INT NOT NULL DEFAULT 0,
    grace_minutes INT NOT NULL DEFAULT 5,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(org_id, name),
    CHECK (break_minutes >= 0),
    CHECK (grace_minutes >= 0)
);
CREATE INDEX IF NOT EXISTS idx_shifts_org ON shifts(org_id);

CREATE TABLE IF NOT EXISTS schedule_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    department_id UUID REFERENCES departments(id) ON DELETE CASCADE,
    shift_id UUID NOT NULL REFERENCES shifts(id) ON DELETE CASCADE,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    day_of_week INT CHECK (day_of_week BETWEEN 0 AND 6),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (user_id IS NOT NULL OR department_id IS NOT NULL),
    CHECK (end_date >= start_date)
);
CREATE INDEX IF NOT EXISTS idx_schedule_org_dates ON schedule_assignments(org_id, start_date, end_date);
CREATE INDEX IF NOT EXISTS idx_schedule_user ON schedule_assignments(user_id);
CREATE INDEX IF NOT EXISTS idx_schedule_department ON schedule_assignments(department_id);

CREATE TABLE IF NOT EXISTS attendance_qr_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    branch_id UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    token TEXT NOT NULL UNIQUE,
    label TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    require_gps BOOLEAN NOT NULL DEFAULT TRUE,
    allowed_radius_m INT,
    scan_count INT NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at),
    CHECK (allowed_radius_m IS NULL OR allowed_radius_m > 0)
);
CREATE INDEX IF NOT EXISTS idx_qr_tokens_org_token ON attendance_qr_tokens(org_id, token);

ALTER TABLE attendance_events ADD COLUMN IF NOT EXISTS branch_id UUID REFERENCES branches(id) ON DELETE SET NULL;
ALTER TABLE attendance_events ADD COLUMN IF NOT EXISTS qr_token_id UUID REFERENCES attendance_qr_tokens(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS bank_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bank_name TEXT NOT NULL,
    account_name TEXT NOT NULL,
    account_number TEXT NOT NULL,
    currency CHAR(3) NOT NULL DEFAULT 'USD',
    is_primary BOOLEAN NOT NULL DEFAULT TRUE,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(org_id, user_id, account_number)
);
CREATE INDEX IF NOT EXISTS idx_bank_accounts_user ON bank_accounts(user_id);

CREATE TABLE IF NOT EXISTS bank_transfer_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    payroll_run_id UUID NOT NULL REFERENCES payroll_runs(id) ON DELETE CASCADE,
    provider TEXT NOT NULL DEFAULT 'manual_csv',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','submitted','processing','completed','failed','cancelled')),
    total_items INT NOT NULL DEFAULT 0,
    total_cents BIGINT NOT NULL DEFAULT 0,
    provider_reference TEXT,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    submitted_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    error_msg TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_bank_batches_org ON bank_transfer_batches(org_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_bank_batches_run ON bank_transfer_batches(payroll_run_id);

CREATE TABLE IF NOT EXISTS bank_transfer_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    batch_id UUID NOT NULL REFERENCES bank_transfer_batches(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bank_account_id UUID REFERENCES bank_accounts(id) ON DELETE SET NULL,
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    currency CHAR(3) NOT NULL DEFAULT 'USD',
    status TEXT NOT NULL DEFAULT 'ready',
    provider_reference TEXT,
    error_msg TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_bank_items_batch ON bank_transfer_items(batch_id);

CREATE TABLE IF NOT EXISTS ewa_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    currency CHAR(3) NOT NULL DEFAULT 'USD',
    reason TEXT,
    status approval_status NOT NULL DEFAULT 'pending',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    review_note TEXT
);
CREATE INDEX IF NOT EXISTS idx_ewa_org_status ON ewa_requests(org_id, status, requested_at DESC);
CREATE INDEX IF NOT EXISTS idx_ewa_user_time ON ewa_requests(user_id, requested_at DESC);
CREATE INDEX IF NOT EXISTS idx_ewa_user_status_month ON ewa_requests(org_id, user_id, status, requested_at DESC);

ALTER TABLE payroll_items ADD COLUMN IF NOT EXISTS ewa_deduction_cents BIGINT NOT NULL DEFAULT 0;


-- Performance indexes for dashboard/report queries.
CREATE INDEX IF NOT EXISTS idx_payroll_runs_org_month ON payroll_runs(org_id, month DESC);
CREATE INDEX IF NOT EXISTS idx_payroll_items_user ON payroll_items(user_id);
CREATE INDEX IF NOT EXISTS idx_qr_tokens_expiry ON attendance_qr_tokens(org_id, active, expires_at);
