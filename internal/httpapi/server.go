package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/hengk7401/checkinme-go-api/internal/cache"
	"github.com/hengk7401/checkinme-go-api/internal/config"
	"github.com/hengk7401/checkinme-go-api/internal/middleware"
	"github.com/hengk7401/checkinme-go-api/internal/security"
	"github.com/hengk7401/checkinme-go-api/internal/services"
)

type Server struct {
	cfg      config.Config
	db       *pgxpool.Pool
	telegram *services.TelegramClient
	cache    *cache.MemoryCache
	asyncSem chan struct{}
	rateMu   sync.Mutex
	rateMap  map[string]*rateBucket
}

type rateBucket struct {
	tokens  float64
	updated time.Time
}

func NewServer(cfg config.Config, db *pgxpool.Pool, telegram *services.TelegramClient) *Server {
	limit := cfg.AsyncWorkerLimit
	if limit <= 0 {
		limit = 8
	}
	return &Server{cfg: cfg, db: db, telegram: telegram, cache: cache.NewMemoryCache(), asyncSem: make(chan struct{}, limit), rateMap: make(map[string]*rateBucket)}
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(recoverMiddleware)
	r.Use(securityHeadersMiddleware)
	r.Use(s.requestTimeoutMiddleware)
	r.Use(s.rateLimitMiddleware)
	r.Use(s.requestLogMiddleware)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   s.cfg.CorsAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Device-Secret", "X-Device-Webhook-Secret", "Idempotency-Key"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Get("/health", s.health)
	r.Get("/ready", s.health)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/setup", s.setup)
		r.Post("/auth/login", s.login)
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(s.cfg.JWTSecret))
			r.Get("/me", s.me)

			r.Get("/employees", s.listEmployees)
			r.With(middleware.RequireRoles("owner", "admin", "manager")).Post("/employees", s.createEmployee)
			r.With(middleware.RequireRoles("owner", "admin", "manager")).Patch("/employees/{id}", s.updateEmployee)

			r.Get("/branches", s.listBranches)
			r.With(middleware.RequireRoles("owner", "admin")).Post("/branches", s.createBranch)

			r.Get("/departments", s.listDepartments)
			r.With(middleware.RequireRoles("owner", "admin", "manager")).Post("/departments", s.createDepartment)
			r.Get("/shifts", s.listShifts)
			r.With(middleware.RequireRoles("owner", "admin", "manager")).Post("/shifts", s.createShift)
			r.Get("/schedule-assignments", s.listScheduleAssignments)
			r.With(middleware.RequireRoles("owner", "admin", "manager")).Post("/schedule-assignments", s.createScheduleAssignment)

			r.Post("/attendance/clock", s.clockAttendance)
			r.Post("/attendance/face-scan", s.faceScanClock)
			r.With(middleware.RequireRoles("owner", "admin", "manager")).Post("/attendance/qr-tokens", s.createAttendanceQRToken)
			r.Get("/attendance/today", s.attendanceToday)
			r.Get("/attendance/sessions", s.attendanceSessions)
			r.With(middleware.RequireRoles("owner", "admin", "manager")).Get("/attendance/fraud-alerts", s.attendanceFraudAlerts)
			r.With(middleware.RequireRoles("owner", "admin", "manager")).Patch("/attendance/fraud-alerts/{id}/review", s.reviewAttendanceFraudAlert)

			r.Post("/leave/requests", s.createLeaveRequest)
			r.Get("/leave/requests", s.listLeaveRequests)
			r.With(middleware.RequireRoles("owner", "admin", "manager")).Patch("/leave/requests/{id}/approve", s.reviewLeaveRequest)

			r.Post("/overtime/requests", s.createOvertimeRequest)
			r.Get("/overtime/requests", s.listOvertimeRequests)
			r.With(middleware.RequireRoles("owner", "admin", "manager")).Patch("/overtime/requests/{id}/approve", s.reviewOvertimeRequest)

			r.With(middleware.RequireRoles("owner", "admin", "manager", "sales")).Get("/customers", s.listCustomers)
			r.With(middleware.RequireRoles("owner", "admin", "manager", "sales")).Post("/customers", s.createCustomer)
			r.With(middleware.RequireRoles("owner", "admin", "manager", "sales")).Post("/sales/visits/checkin", s.salesVisitCheckIn)
			r.With(middleware.RequireRoles("owner", "admin", "manager", "sales")).Patch("/sales/visits/{id}/checkout", s.salesVisitCheckOut)
			r.With(middleware.RequireRoles("owner", "admin", "manager", "sales")).Get("/sales/visits", s.listSalesVisits)
			r.Get("/sales/summary", s.salesDailySummary)
			r.With(middleware.RequireRoles("owner", "admin", "manager")).Post("/sales/summary/telegram", s.sendSalesDailyTelegramSummary)
			r.With(middleware.RequireRoles("owner", "admin", "manager")).Post("/kpis", s.upsertKPI)
			r.Get("/kpis", s.listKPIs)

			r.With(middleware.RequireRoles("owner", "admin")).Get("/payroll/rules", s.getPayrollRules)
			r.With(middleware.RequireRoles("owner", "admin")).Put("/payroll/rules", s.putPayrollRules)
			r.With(middleware.RequireRoles("owner", "admin")).Post("/payroll/runs", s.createPayrollRun)
			r.With(middleware.RequireRoles("owner", "admin")).Get("/payroll/runs", s.listPayrollRuns)
			r.With(middleware.RequireRoles("owner", "admin")).Get("/payroll/runs/{id}", s.getPayrollRun)
			r.With(middleware.RequireRoles("owner", "admin")).Post("/payroll/runs/{id}/approve", s.approvePayrollRun)
			r.With(middleware.RequireRoles("owner", "admin")).Post("/payroll/runs/{id}/payout-export", s.payrollPayoutExport)
			r.With(middleware.RequireRoles("owner", "admin")).Get("/payroll/runs/{id}/export.csv", s.payrollRunCSVExport)
			r.With(middleware.RequireRoles("owner", "admin")).Get("/payroll/runs/{id}/bank-statement.csv", s.payrollBankStatementCSVExport)
			r.Get("/payroll/runs/{id}/payslips/{user_id}", s.getDigitalPayslip)
			r.With(middleware.RequireRoles("owner", "admin")).Post("/payroll/runs/{id}/bank-batches", s.createBankTransferBatch)
			r.Get("/bank/accounts", s.listBankAccounts)
			r.Post("/bank/accounts", s.createBankAccount)
			r.With(middleware.RequireRoles("owner", "admin")).Get("/bank/batches", s.listBankTransferBatches)
			r.With(middleware.RequireRoles("owner", "admin")).Post("/bank/batches/{id}/mark-submitted", s.markBankBatchSubmitted)
			r.Post("/ewa/requests", s.createEWARequest)
			r.Get("/ewa/requests", s.listEWARequests)
			r.With(middleware.RequireRoles("owner", "admin", "manager")).Patch("/ewa/requests/{id}/approve", s.reviewEWARequest)

			r.Get("/reports/summary", s.reportSummary)
			r.Get("/reports/insights", s.reportInsights)
			r.With(middleware.RequireRoles("owner", "admin", "manager")).Post("/reports/telegram-daily", s.sendDailyTelegramReport)
			r.With(middleware.RequireRoles("owner", "admin")).Get("/system/performance", s.systemPerformance)
		})
		r.Post("/devices/face-events", s.faceDeviceEvent)
		r.Post("/device/face-webhook", s.faceDeviceEvent)
	})
	return r
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	err := s.db.Ping(ctx)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       err == nil,
		"database": err == nil,
		"time":     time.Now().UTC(),
	})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrgName   string `json:"org_name"`
		AdminName string `json:"admin_name"`
		Email     string `json:"email"`
		Password  string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if strings.TrimSpace(req.OrgName) == "" || strings.TrimSpace(req.AdminName) == "" || req.Email == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "org_name, admin_name, valid email and password >= 8 chars are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var count int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM organizations`).Scan(&count); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count > 0 {
		writeError(w, http.StatusConflict, "setup already completed")
		return
	}
	hash, err := security.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback(ctx)
	var orgID, userID string
	if err := tx.QueryRow(ctx, `INSERT INTO organizations(name, timezone) VALUES($1,$2) RETURNING id`, req.OrgName, s.cfg.DefaultTimezone).Scan(&orgID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.QueryRow(ctx, `INSERT INTO users(org_id, full_name, email, password_hash, role, active) VALUES($1,$2,$3,$4,'owner',true) RETURNING id`, orgID, req.AdminName, req.Email, hash).Scan(&userID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := seedPayrollDefaults(ctx, tx, orgID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	token, _ := security.SignToken(s.cfg.JWTSecret, userID, orgID, "owner", req.AdminName, req.Email)
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "org_id": orgID, "user_id": userID, "token": token})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var u struct {
		ID, OrgID, FullName, Email, PasswordHash, Role string
		Active                                         bool
	}
	err := s.db.QueryRow(ctx, `SELECT id, org_id, full_name, email, password_hash, role::text, active FROM users WHERE email=$1`, req.Email).Scan(&u.ID, &u.OrgID, &u.FullName, &u.Email, &u.PasswordHash, &u.Role, &u.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !u.Active || !security.CheckPassword(u.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	token, err := security.SignToken(s.cfg.JWTSecret, u.ID, u.OrgID, u.Role, u.FullName, u.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": token, "user": map[string]any{"id": u.ID, "org_id": u.OrgID, "name": u.FullName, "email": u.Email, "role": u.Role}})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": claims})
}

func (s *Server) listEmployees(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	limit, offset := limitOffset(r, 100, 500)
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	args := []any{claims.OrgID}
	q := `SELECT id, full_name, email, phone, role::text, branch_id, manager_id, base_salary_cents, currency, active, created_at FROM users WHERE org_id=$1`
	if claims.Role == "employee" || claims.Role == "sales" {
		q += ` AND id=$2`
		args = append(args, claims.UserID)
	}
	if search != "" {
		q += fmt.Sprintf(` AND (full_name ILIKE $%d OR email ILIKE $%d OR COALESCE(employee_code,'') ILIKE $%d)`, len(args)+1, len(args)+1, len(args)+1)
		args = append(args, "%"+search+"%")
	}
	q += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name, email, role, currency string
		var phone, branchID, managerID *string
		var salary int64
		var active bool
		var created time.Time
		if err := rows.Scan(&id, &name, &email, &phone, &role, &branchID, &managerID, &salary, &currency, &active, &created); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "full_name": name, "email": email, "phone": phone, "role": role, "branch_id": branchID, "manager_id": managerID, "base_salary_cents": salary, "currency": currency, "active": active, "created_at": created})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "employees": items})
}

func (s *Server) createEmployee(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	var req struct {
		FullName        string  `json:"full_name"`
		Email           string  `json:"email"`
		Phone           string  `json:"phone"`
		Password        string  `json:"password"`
		Role            string  `json:"role"`
		BranchID        *string `json:"branch_id"`
		ManagerID       *string `json:"manager_id"`
		DepartmentID    *string `json:"department_id"`
		EmployeeCode    string  `json:"employee_code"`
		BaseSalaryCents int64   `json:"base_salary_cents"`
		Currency        string  `json:"currency"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.FullName = strings.TrimSpace(req.FullName)
	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	if req.Currency == "" {
		req.Currency = "USD"
	}
	if len(req.Currency) != 3 {
		writeError(w, 400, "currency must be a 3-letter ISO code")
		return
	}
	if req.BaseSalaryCents < 0 {
		writeError(w, 400, "base_salary_cents cannot be negative")
		return
	}
	if req.Role == "" {
		req.Role = "employee"
	}
	if !validRole(req.Role) {
		writeError(w, 400, "invalid role")
		return
	}
	if req.FullName == "" || req.Email == "" || !strings.Contains(req.Email, "@") || len(req.Password) < 8 {
		writeError(w, 400, "full_name, email, and password >= 8 are required")
		return
	}
	hash, err := security.HashPassword(req.Password)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var id string
	err = s.db.QueryRow(ctx, `INSERT INTO users(org_id, branch_id, manager_id, department_id, employee_code, full_name, email, phone, password_hash, role, base_salary_cents, currency) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`, claims.OrgID, req.BranchID, req.ManagerID, req.DepartmentID, nullIfEmpty(req.EmployeeCode), req.FullName, req.Email, nullIfEmpty(req.Phone), hash, req.Role, req.BaseSalaryCents, req.Currency).Scan(&id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	s.invalidateOrgCaches(claims.OrgID)
	writeJSON(w, 201, map[string]any{"ok": true, "id": id})
}

func (s *Server) updateEmployee(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	id := chi.URLParam(r, "id")
	var req struct {
		FullName        string  `json:"full_name"`
		Phone           string  `json:"phone"`
		Role            string  `json:"role"`
		Currency        string  `json:"currency"`
		BranchID        *string `json:"branch_id"`
		ManagerID       *string `json:"manager_id"`
		DepartmentID    *string `json:"department_id"`
		EmployeeCode    string  `json:"employee_code"`
		BaseSalaryCents *int64  `json:"base_salary_cents"`
		Active          *bool   `json:"active"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Role != "" && !validRole(req.Role) {
		writeError(w, 400, "invalid role")
		return
	}
	if req.Currency != "" {
		req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
		if len(req.Currency) != 3 {
			writeError(w, 400, "currency must be a 3-letter ISO code")
			return
		}
	}
	if req.BaseSalaryCents != nil && *req.BaseSalaryCents < 0 {
		writeError(w, 400, "base_salary_cents cannot be negative")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	cmd, err := s.db.Exec(ctx, `UPDATE users SET full_name=COALESCE(NULLIF($1,''),full_name), phone=COALESCE($2,phone), role=COALESCE(NULLIF($3,'')::user_role,role), branch_id=COALESCE($4,branch_id), manager_id=COALESCE($5,manager_id), department_id=COALESCE($6,department_id), employee_code=COALESCE($7,employee_code), base_salary_cents=COALESCE($8,base_salary_cents), currency=COALESCE(NULLIF($9,''),currency), active=COALESCE($10,active), updated_at=now() WHERE id=$11 AND org_id=$12`, req.FullName, nullIfEmpty(req.Phone), req.Role, req.BranchID, req.ManagerID, req.DepartmentID, nullIfEmpty(req.EmployeeCode), req.BaseSalaryCents, req.Currency, req.Active, id, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if cmd.RowsAffected() == 0 {
		writeError(w, 404, "employee not found")
		return
	}
	s.invalidateOrgCaches(claims.OrgID)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) listBranches(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT id, name, address, lat, lng, gps_radius_m, active FROM branches WHERE org_id=$1 ORDER BY name`, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name string
		var address *string
		var lat, lng *float64
		var radius int
		var active bool
		if err := rows.Scan(&id, &name, &address, &lat, &lng, &radius, &active); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "address": address, "lat": lat, "lng": lng, "gps_radius_m": radius, "active": active})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "branches": items})
}

func (s *Server) createBranch(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	var req struct {
		Name       string   `json:"name"`
		Address    string   `json:"address"`
		Lat        *float64 `json:"lat"`
		Lng        *float64 `json:"lng"`
		GPSRadiusM int      `json:"gps_radius_m"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.GPSRadiusM <= 0 {
		req.GPSRadiusM = 150
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, 400, "name is required")
		return
	}
	if !validOptionalLatLng(req.Lat, req.Lng) {
		writeError(w, 400, "lat/lng must both be provided and valid")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var id string
	err := s.db.QueryRow(ctx, `INSERT INTO branches(org_id, name, address, lat, lng, gps_radius_m) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, claims.OrgID, req.Name, nullIfEmpty(req.Address), req.Lat, req.Lng, req.GPSRadiusM).Scan(&id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "id": id})
}

type attendanceClockRequest struct {
	Kind         string   `json:"kind"`
	Lat          *float64 `json:"lat"`
	Lng          *float64 `json:"lng"`
	GPSAccuracyM *int     `json:"gps_accuracy_m"`
	Source       string   `json:"source"`
	QRToken      string   `json:"qr_token"`
	FaceScore    *float64 `json:"face_score"`
	MockLocation *bool    `json:"mock_location"`
	DeviceID     string   `json:"device_id"`
	Note         string   `json:"note"`
}

type attendanceFraudAssessment struct {
	Status         string   `json:"status"`
	Score          int      `json:"score"`
	Reasons        []string `json:"reasons"`
	Blocked        bool     `json:"blocked"`
	DistanceM      *int     `json:"distance_m,omitempty"`
	TravelSpeedKPH *float64 `json:"travel_speed_kph,omitempty"`
	ReviewRequired bool     `json:"review_required"`
}

func (s *Server) clockAttendance(w http.ResponseWriter, r *http.Request) {
	s.handleAttendanceClock(w, r, false)
}

func (s *Server) faceScanClock(w http.ResponseWriter, r *http.Request) {
	s.handleAttendanceClock(w, r, true)
}

func (s *Server) handleAttendanceClock(w http.ResponseWriter, r *http.Request, requireFace bool) {
	claims := middleware.Claims(r)
	var req attendanceClockRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Kind = strings.ToLower(strings.TrimSpace(req.Kind))
	req.Source = strings.ToLower(strings.TrimSpace(req.Source))
	if req.Kind != "in" && req.Kind != "out" {
		writeError(w, 400, "kind must be in or out")
		return
	}
	if requireFace {
		if req.FaceScore == nil {
			writeError(w, 400, "face_score is required for face scan clocking")
			return
		}
		if req.Source == "" {
			req.Source = "face_scan"
		}
	}
	if !validOptionalLatLng(req.Lat, req.Lng) {
		writeError(w, 400, "lat/lng must both be provided and valid")
		return
	}
	if req.GPSAccuracyM != nil && *req.GPSAccuracyM < 0 {
		writeError(w, 400, "gps_accuracy_m cannot be negative")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer tx.Rollback(ctx)
	source, branchID, qrTokenID, status, reason := s.verifyAttendanceEvidence(ctx, tx, claims.OrgID, claims.UserID, req)
	if reason != "" {
		writeError(w, status, reason)
		return
	}
	var eventID string
	now := time.Now().UTC()
	fraud, err := s.assessAttendanceFraud(ctx, tx, claims.OrgID, claims.UserID, req.Kind, source, req, branchID, qrTokenID, now)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if fraud.Blocked {
		_ = s.writeAudit(ctx, tx, claims.OrgID, claims.UserID, "attendance.blocked_by_fraud", "user", claims.UserID, map[string]any{"kind": req.Kind, "source": source, "score": fraud.Score, "reasons": fraud.Reasons})
		_ = tx.Commit(ctx)
		writeJSON(w, 403, map[string]any{"ok": false, "error": "attendance blocked by anti-fraud rules", "fraud": fraud})
		return
	}
	deviceID := strings.TrimSpace(req.DeviceID)
	err = tx.QueryRow(ctx, `INSERT INTO attendance_events(org_id, user_id, kind, event_at, lat, lng, gps_accuracy_m, source, branch_id, qr_token_id, face_score, mock_location, device_id, fraud_status, fraud_score, fraud_reasons, fraud_distance_m, fraud_speed_kph, note) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,COALESCE($12,false),NULLIF($13,''),$14,$15,$16,$17,$18,$19) RETURNING id`, claims.OrgID, claims.UserID, req.Kind, now, req.Lat, req.Lng, req.GPSAccuracyM, source, branchID, qrTokenID, req.FaceScore, req.MockLocation, deviceID, fraud.Status, fraud.Score, fraud.Reasons, fraud.DistanceM, fraud.TravelSpeedKPH, nullIfEmpty(req.Note)).Scan(&eventID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	var sessionID string
	if req.Kind == "in" {
		var existing string
		err = tx.QueryRow(ctx, `SELECT id FROM attendance_sessions WHERE org_id=$1 AND user_id=$2 AND clock_out_at IS NULL ORDER BY clock_in_at DESC LIMIT 1 FOR UPDATE`, claims.OrgID, claims.UserID).Scan(&existing)
		if err == nil {
			writeError(w, 409, "already clocked in")
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 500, err.Error())
			return
		}
		late := lateMinutes(now)
		err = tx.QueryRow(ctx, `INSERT INTO attendance_sessions(org_id, user_id, clock_in_id, clock_in_at, late_minutes) VALUES($1,$2,$3,$4,$5) RETURNING id`, claims.OrgID, claims.UserID, eventID, now, late).Scan(&sessionID)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
	} else {
		var clockInAt time.Time
		err = tx.QueryRow(ctx, `SELECT id, clock_in_at FROM attendance_sessions WHERE org_id=$1 AND user_id=$2 AND clock_out_at IS NULL ORDER BY clock_in_at DESC LIMIT 1 FOR UPDATE`, claims.OrgID, claims.UserID).Scan(&sessionID, &clockInAt)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 409, "no open clock-in session")
			return
		}
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		total := int(now.Sub(clockInAt).Minutes())
		if total < 0 {
			total = 0
		}
		overtime := total - 8*60
		if overtime < 0 {
			overtime = 0
		}
		_, err = tx.Exec(ctx, `UPDATE attendance_sessions SET clock_out_id=$1, clock_out_at=$2, total_minutes=$3, overtime_minutes=$4, updated_at=now() WHERE id=$5`, eventID, now, total, overtime, sessionID)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
	}
	if fraud.ReviewRequired {
		_ = s.writeAudit(ctx, tx, claims.OrgID, claims.UserID, "attendance.fraud_warning", "attendance_event", eventID, map[string]any{"score": fraud.Score, "reasons": fraud.Reasons})
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	s.invalidateOrgCaches(claims.OrgID)
	s.runAsync(func() {
		s.sendAttendanceTelegram(context.Background(), claims.OrgID, claims.Name, req.Kind, now, fraud)
	})
	writeJSON(w, 201, map[string]any{"ok": true, "event_id": eventID, "session_id": sessionID, "kind": req.Kind, "source": source, "branch_id": branchID, "event_at": now, "fraud": fraud})
}

func (s *Server) sendAttendanceTelegram(ctx context.Context, orgID, name, kind string, at time.Time, fraud attendanceFraudAssessment) {
	chatID := s.orgTelegramChatID(ctx, orgID)
	icon := "✅"
	label := "Clock In"
	if kind == "out" {
		icon = "🏁"
		label = "Clock Out"
	}
	text := fmt.Sprintf("%s <b>CheckinMe %s</b>\n👤 %s\n🕒 %s", icon, label, services.EscapeHTML(name), at.Format("2006-01-02 15:04 MST"))
	if fraud.Status != "normal" {
		text += fmt.Sprintf("\n⚠️ Fraud status: <b>%s</b> (%d)\nReason: %s", services.EscapeHTML(fraud.Status), fraud.Score, services.EscapeHTML(strings.Join(fraud.Reasons, "; ")))
	}
	_ = s.telegram.SendMessage(ctx, chatID, text)
}

func (s *Server) orgTelegramChatID(ctx context.Context, orgID string) string {
	key := "org_telegram:" + orgID
	var cached string
	if s.cache.GetJSON(key, &cached) {
		return cached
	}
	var chatID *string
	_ = s.db.QueryRow(ctx, `SELECT telegram_chat_id FROM organizations WHERE id=$1`, orgID).Scan(&chatID)
	if chatID != nil {
		s.cache.SetJSON(key, *chatID, time.Duration(maxInt(30, s.cfg.CacheTTLSeconds))*time.Second)
		return *chatID
	}
	s.cache.SetJSON(key, "", 30*time.Second)
	return ""
}

func (s *Server) attendanceToday(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	date := queryDate(r, "date", time.Now())
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	args := []any{claims.OrgID, start, end}
	q := `SELECT e.id, e.user_id, u.full_name, e.kind::text, e.event_at, e.lat, e.lng, e.source, e.fraud_status, e.fraud_score, e.note FROM attendance_events e JOIN users u ON u.id=e.user_id WHERE e.org_id=$1 AND e.event_at >= $2 AND e.event_at < $3`
	if claims.Role == "employee" || claims.Role == "sales" {
		q += ` AND e.user_id=$4`
		args = append(args, claims.UserID)
	}
	q += ` ORDER BY e.event_at DESC`
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, userID, name, kind, source, fraudStatus string
		var at time.Time
		var lat, lng *float64
		var fraudScore int
		var note *string
		if err := rows.Scan(&id, &userID, &name, &kind, &at, &lat, &lng, &source, &fraudStatus, &fraudScore, &note); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "user_id": userID, "full_name": name, "kind": kind, "event_at": at, "lat": lat, "lng": lng, "source": source, "fraud_status": fraudStatus, "fraud_score": fraudScore, "note": note})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "events": items})
}

func (s *Server) attendanceSessions(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	from, to := queryRange(r)
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	args := []any{claims.OrgID, from, to}
	q := `SELECT s.id, s.user_id, u.full_name, s.clock_in_at, s.clock_out_at, s.total_minutes, s.late_minutes, s.overtime_minutes, ci.fraud_status, ci.fraud_score FROM attendance_sessions s JOIN users u ON u.id=s.user_id JOIN attendance_events ci ON ci.id=s.clock_in_id WHERE s.org_id=$1 AND s.clock_in_at >= $2 AND s.clock_in_at < $3`
	if claims.Role == "employee" || claims.Role == "sales" {
		q += fmt.Sprintf(` AND s.user_id=$%d`, len(args)+1)
		args = append(args, claims.UserID)
	} else if userID != "" {
		q += fmt.Sprintf(` AND s.user_id=$%d`, len(args)+1)
		args = append(args, userID)
	}
	limit, offset := limitOffset(r, 100, 500)
	q += fmt.Sprintf(` ORDER BY s.clock_in_at DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, uid, name, fraudStatus string
		var in time.Time
		var out *time.Time
		var total, late, ot, fraudScore int
		if err := rows.Scan(&id, &uid, &name, &in, &out, &total, &late, &ot, &fraudStatus, &fraudScore); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "user_id": uid, "full_name": name, "clock_in_at": in, "clock_out_at": out, "total_minutes": total, "late_minutes": late, "overtime_minutes": ot, "fraud_status": fraudStatus, "fraud_score": fraudScore})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "sessions": items, "limit": limit, "offset": offset})
}

func (s *Server) attendanceFraudAlerts(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	from, to := queryRange(r)
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	limit, offset := limitOffset(r, 100, 500)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	args := []any{claims.OrgID, from, to}
	q := `SELECT e.id,e.user_id,u.full_name,e.kind::text,e.event_at,e.source,e.lat,e.lng,e.gps_accuracy_m,e.face_score,e.mock_location,e.fraud_status,e.fraud_score,e.fraud_reasons,e.fraud_distance_m,e.fraud_speed_kph,e.fraud_reviewed_at,e.fraud_review_note
FROM attendance_events e JOIN users u ON u.id=e.user_id
WHERE e.org_id=$1 AND e.event_at >= $2 AND e.event_at < $3 AND e.fraud_status <> 'normal'`
	if status != "" {
		q += fmt.Sprintf(` AND e.fraud_status=$%d`, len(args)+1)
		args = append(args, status)
	}
	if userID != "" {
		q += fmt.Sprintf(` AND e.user_id=$%d`, len(args)+1)
		args = append(args, userID)
	}
	q += fmt.Sprintf(` ORDER BY e.fraud_score DESC,e.event_at DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, uid, name, kind, source, fraudStatus string
		var at time.Time
		var lat, lng, faceScore, speed *float64
		var gpsAccuracy, score, distance *int
		var mock bool
		var reasons []string
		var reviewedAt *time.Time
		var reviewNote *string
		if err := rows.Scan(&id, &uid, &name, &kind, &at, &source, &lat, &lng, &gpsAccuracy, &faceScore, &mock, &fraudStatus, &score, &reasons, &distance, &speed, &reviewedAt, &reviewNote); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "user_id": uid, "full_name": name, "kind": kind, "event_at": at, "source": source, "lat": lat, "lng": lng, "gps_accuracy_m": gpsAccuracy, "face_score": faceScore, "mock_location": mock, "fraud_status": fraudStatus, "fraud_score": score, "fraud_reasons": reasons, "fraud_distance_m": distance, "fraud_speed_kph": speed, "reviewed_at": reviewedAt, "review_note": reviewNote})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "alerts": items, "limit": limit, "offset": offset})
}

func (s *Server) reviewAttendanceFraudAlert(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	id := chi.URLParam(r, "id")
	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	if req.Status != "reviewed" && req.Status != "false_positive" && req.Status != "confirmed" {
		writeError(w, 400, "status must be reviewed, false_positive, or confirmed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer tx.Rollback(ctx)
	cmd, err := tx.Exec(ctx, `UPDATE attendance_events SET fraud_reviewed_by=$1,fraud_reviewed_at=now(),fraud_review_note=$2,fraud_status=$3 WHERE id=$4 AND org_id=$5 AND fraud_status <> 'normal'`, claims.UserID, nullIfEmpty(req.Note), req.Status, id, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if cmd.RowsAffected() == 0 {
		writeError(w, 404, "fraud alert not found")
		return
	}
	_ = s.writeAudit(ctx, tx, claims.OrgID, claims.UserID, "attendance.fraud_review", "attendance_event", id, map[string]any{"status": req.Status, "note": req.Note})
	if err := tx.Commit(ctx); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	s.invalidateOrgCaches(claims.OrgID)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) createLeaveRequest(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	var req struct {
		LeaveType string `json:"leave_type"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
		Reason    string `json:"reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	start, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		writeError(w, 400, "invalid start_date")
		return
	}
	end, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		writeError(w, 400, "invalid end_date")
		return
	}
	if end.Before(start) {
		writeError(w, 400, "end_date must be after start_date")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var id string
	err = s.db.QueryRow(ctx, `INSERT INTO leave_requests(org_id, user_id, leave_type, start_date, end_date, reason) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, claims.OrgID, claims.UserID, valueOr(req.LeaveType, "annual"), start, end, nullIfEmpty(req.Reason)).Scan(&id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "id": id})
}

func (s *Server) listLeaveRequests(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	args := []any{claims.OrgID}
	q := `SELECT l.id, l.user_id, u.full_name, l.leave_type, l.start_date, l.end_date, l.reason, l.status::text, l.created_at FROM leave_requests l JOIN users u ON u.id=l.user_id WHERE l.org_id=$1`
	if status != "" {
		q += ` AND l.status=$2`
		args = append(args, status)
	}
	if claims.Role == "employee" || claims.Role == "sales" {
		q += fmt.Sprintf(` AND l.user_id=$%d`, len(args)+1)
		args = append(args, claims.UserID)
	}
	q += ` ORDER BY l.created_at DESC LIMIT 200`
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, uid, name, typ, reason, st string
		var start, end, created time.Time
		if err := rows.Scan(&id, &uid, &name, &typ, &start, &end, &reason, &st, &created); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "user_id": uid, "full_name": name, "leave_type": typ, "start_date": start.Format("2006-01-02"), "end_date": end.Format("2006-01-02"), "reason": reason, "status": st, "created_at": created})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "requests": items})
}

func (s *Server) reviewLeaveRequest(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	id := chi.URLParam(r, "id")
	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Status != "approved" && req.Status != "rejected" {
		writeError(w, 400, "status must be approved or rejected")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	cmd, err := s.db.Exec(ctx, `UPDATE leave_requests SET status=$1, reviewed_by=$2, reviewed_at=now(), review_note=$3 WHERE id=$4 AND org_id=$5`, req.Status, claims.UserID, nullIfEmpty(req.Note), id, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if cmd.RowsAffected() == 0 {
		writeError(w, 404, "leave request not found")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) createOvertimeRequest(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	var req struct {
		WorkDate string `json:"work_date"`
		Reason   string `json:"reason"`
		Minutes  int    `json:"minutes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	day, err := time.Parse("2006-01-02", req.WorkDate)
	if err != nil {
		writeError(w, 400, "invalid work_date")
		return
	}
	if req.Minutes <= 0 {
		writeError(w, 400, "minutes must be positive")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var id string
	err = s.db.QueryRow(ctx, `INSERT INTO overtime_requests(org_id,user_id,work_date,minutes,reason) VALUES($1,$2,$3,$4,$5) RETURNING id`, claims.OrgID, claims.UserID, day, req.Minutes, nullIfEmpty(req.Reason)).Scan(&id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "id": id})
}

func (s *Server) listOvertimeRequests(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	args := []any{claims.OrgID}
	q := `SELECT o.id,o.user_id,u.full_name,o.work_date,o.minutes,o.reason,o.status::text,o.created_at FROM overtime_requests o JOIN users u ON u.id=o.user_id WHERE o.org_id=$1`
	if status != "" {
		q += ` AND o.status=$2`
		args = append(args, status)
	}
	if claims.Role == "employee" || claims.Role == "sales" {
		q += fmt.Sprintf(` AND o.user_id=$%d`, len(args)+1)
		args = append(args, claims.UserID)
	}
	q += ` ORDER BY o.created_at DESC LIMIT 200`
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, uid, name, st string
		var reason *string
		var work, created time.Time
		var mins int
		if err := rows.Scan(&id, &uid, &name, &work, &mins, &reason, &st, &created); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "user_id": uid, "full_name": name, "work_date": work.Format("2006-01-02"), "minutes": mins, "reason": reason, "status": st, "created_at": created})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "requests": items})
}

func (s *Server) reviewOvertimeRequest(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	id := chi.URLParam(r, "id")
	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Status != "approved" && req.Status != "rejected" {
		writeError(w, 400, "status must be approved or rejected")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	cmd, err := s.db.Exec(ctx, `UPDATE overtime_requests SET status=$1, reviewed_by=$2, reviewed_at=now(), review_note=$3 WHERE id=$4 AND org_id=$5`, req.Status, claims.UserID, nullIfEmpty(req.Note), id, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if cmd.RowsAffected() == 0 {
		writeError(w, 404, "overtime request not found")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) createCustomer(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	var req struct {
		Name       string   `json:"name"`
		Phone      string   `json:"phone"`
		Address    string   `json:"address"`
		AssignedTo *string  `json:"assigned_to"`
		Lat        *float64 `json:"lat"`
		Lng        *float64 `json:"lng"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, 400, "name is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	assigned := req.AssignedTo
	if assigned == nil && claims.Role == "sales" {
		assigned = &claims.UserID
	}
	var id string
	err := s.db.QueryRow(ctx, `INSERT INTO customers(org_id,assigned_to,name,phone,address,lat,lng) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, claims.OrgID, assigned, req.Name, nullIfEmpty(req.Phone), nullIfEmpty(req.Address), req.Lat, req.Lng).Scan(&id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "id": id})
}

func (s *Server) listCustomers(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	limit, offset := limitOffset(r, 100, 500)
	args := []any{claims.OrgID}
	q := `SELECT id,assigned_to,name,phone,address,lat,lng,active,created_at FROM customers WHERE org_id=$1`
	if claims.Role == "sales" {
		q += fmt.Sprintf(` AND assigned_to=$%d`, len(args)+1)
		args = append(args, claims.UserID)
	}
	q += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name string
		var assigned, phone, address *string
		var lat, lng *float64
		var active bool
		var created time.Time
		if err := rows.Scan(&id, &assigned, &name, &phone, &address, &lat, &lng, &active, &created); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "assigned_to": assigned, "name": name, "phone": phone, "address": address, "lat": lat, "lng": lng, "active": active, "created_at": created})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "customers": items, "limit": limit, "offset": offset})
}

func (s *Server) salesVisitCheckIn(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	var req struct {
		CustomerID string   `json:"customer_id"`
		Lat        *float64 `json:"lat"`
		Lng        *float64 `json:"lng"`
		Notes      string   `json:"notes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.CustomerID == "" || req.Lat == nil || req.Lng == nil {
		writeError(w, 400, "customer_id, lat and lng are required")
		return
	}
	if !validLatLng(*req.Lat, *req.Lng) {
		writeError(w, 400, "invalid lat/lng")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var cLat, cLng *float64
	err := s.db.QueryRow(ctx, `SELECT lat,lng FROM customers WHERE id=$1 AND org_id=$2`, req.CustomerID, claims.OrgID).Scan(&cLat, &cLng)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "customer not found")
		return
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	var distance *int
	if cLat != nil && cLng != nil {
		d := int(haversineMeters(*req.Lat, *req.Lng, *cLat, *cLng))
		distance = &d
	}
	var id string
	err = s.db.QueryRow(ctx, `INSERT INTO sales_visits(org_id,user_id,customer_id,lat,lng,distance_m,notes) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, claims.OrgID, claims.UserID, req.CustomerID, req.Lat, req.Lng, distance, nullIfEmpty(req.Notes)).Scan(&id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	s.invalidateOrgCaches(claims.OrgID)
	writeJSON(w, 201, map[string]any{"ok": true, "id": id, "distance_m": distance})
}

func (s *Server) salesVisitCheckOut(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	id := chi.URLParam(r, "id")
	var req struct {
		Lat   *float64 `json:"lat"`
		Lng   *float64 `json:"lng"`
		Notes string   `json:"notes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !validOptionalLatLng(req.Lat, req.Lng) {
		writeError(w, 400, "lat/lng must both be provided and valid")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	cmd, err := s.db.Exec(ctx, `UPDATE sales_visits SET check_out_at=now(), checkout_lat=$1, checkout_lng=$2, notes=COALESCE(NULLIF($3,''),notes) WHERE id=$4 AND org_id=$5 AND user_id=$6 AND check_out_at IS NULL`, req.Lat, req.Lng, req.Notes, id, claims.OrgID, claims.UserID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if cmd.RowsAffected() == 0 {
		writeError(w, 404, "open sales visit not found")
		return
	}
	s.invalidateOrgCaches(claims.OrgID)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) listSalesVisits(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	from, to := queryRange(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	args := []any{claims.OrgID, from, to}
	q := `SELECT v.id,v.user_id,u.full_name,v.customer_id,c.name,v.check_in_at,v.check_out_at,v.lat,v.lng,v.distance_m,v.notes FROM sales_visits v JOIN users u ON u.id=v.user_id JOIN customers c ON c.id=v.customer_id WHERE v.org_id=$1 AND v.check_in_at >= $2 AND v.check_in_at < $3`
	if claims.Role == "sales" {
		q += fmt.Sprintf(` AND v.user_id=$%d`, len(args)+1)
		args = append(args, claims.UserID)
	}
	limit, offset := limitOffset(r, 100, 500)
	q += fmt.Sprintf(` ORDER BY v.check_in_at DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, uid, uname, cid, cname string
		var in time.Time
		var out *time.Time
		var lat, lng *float64
		var dist *int
		var notes *string
		if err := rows.Scan(&id, &uid, &uname, &cid, &cname, &in, &out, &lat, &lng, &dist, &notes); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "user_id": uid, "full_name": uname, "customer_id": cid, "customer_name": cname, "check_in_at": in, "check_out_at": out, "lat": lat, "lng": lng, "distance_m": dist, "notes": notes})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "visits": items, "limit": limit, "offset": offset})
}

func (s *Server) upsertKPI(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	var req struct {
		UserID           string `json:"user_id"`
		Month            string `json:"month"`
		VisitsTarget     int    `json:"visits_target"`
		SalesTargetCents int64  `json:"sales_target_cents"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	month, err := parseMonth(req.Month)
	if err != nil {
		writeError(w, 400, "invalid month. use YYYY-MM")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var id string
	err = s.db.QueryRow(ctx, `INSERT INTO kpis(org_id,user_id,month,visits_target,sales_target_cents) VALUES($1,$2,$3,$4,$5) ON CONFLICT(org_id,user_id,month) DO UPDATE SET visits_target=EXCLUDED.visits_target, sales_target_cents=EXCLUDED.sales_target_cents RETURNING id`, claims.OrgID, req.UserID, month, req.VisitsTarget, req.SalesTargetCents).Scan(&id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "id": id})
}

func (s *Server) listKPIs(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	month, err := parseMonth(r.URL.Query().Get("month"))
	if err != nil {
		month = time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	args := []any{claims.OrgID, month}
	q := `SELECT k.id,k.user_id,u.full_name,k.month,k.visits_target,k.sales_target_cents,COALESCE(count(v.id),0) AS visits_done FROM kpis k JOIN users u ON u.id=k.user_id LEFT JOIN sales_visits v ON v.user_id=k.user_id AND date_trunc('month', v.check_in_at)=k.month WHERE k.org_id=$1 AND k.month=$2`
	if claims.Role == "sales" {
		q += ` AND k.user_id=$3`
		args = append(args, claims.UserID)
	}
	q += ` GROUP BY k.id,u.full_name ORDER BY u.full_name`
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, uid, name string
		var m time.Time
		var visits, targetDone int
		var sales int64
		if err := rows.Scan(&id, &uid, &name, &m, &visits, &sales, &targetDone); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "user_id": uid, "full_name": name, "month": m.Format("2006-01"), "visits_target": visits, "sales_target_cents": sales, "visits_done": targetDone})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "kpis": items})
}

func (s *Server) getPayrollRules(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT rule_key, rule_value, description FROM payroll_rules WHERE org_id=$1 AND active=true ORDER BY rule_key`, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	rules := map[string]any{}
	for rows.Next() {
		var k, desc string
		var v float64
		if err := rows.Scan(&k, &v, &desc); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		rules[k] = map[string]any{"value": v, "description": desc}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "rules": rules})
}

func (s *Server) putPayrollRules(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	var req map[string]float64
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer tx.Rollback(ctx)
	for k, v := range req {
		if strings.TrimSpace(k) == "" {
			continue
		}
		_, err = tx.Exec(ctx, `INSERT INTO payroll_rules(org_id,rule_key,rule_value,description) VALUES($1,$2,$3,'custom') ON CONFLICT(org_id,rule_key) DO UPDATE SET rule_value=EXCLUDED.rule_value, active=true`, claims.OrgID, k, v)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) createPayrollRun(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	var req struct {
		Month string `json:"month"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	month, err := parseMonth(req.Month)
	if err != nil {
		writeError(w, 400, "invalid month. use YYYY-MM")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	runID, err := s.calculatePayroll(ctx, claims.OrgID, claims.UserID, month)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "payroll_run_id": runID})
}

func (s *Server) calculatePayroll(ctx context.Context, orgID, actorID string, month time.Time) (string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var existingID, existingStatus string
	err = tx.QueryRow(ctx, `SELECT id,status::text FROM payroll_runs WHERE org_id=$1 AND month=$2 FOR UPDATE`, orgID, month).Scan(&existingID, &existingStatus)
	if err == nil {
		if existingStatus != "draft" {
			return "", fmt.Errorf("payroll run for %s is already %s and cannot be recalculated", month.Format("2006-01"), existingStatus)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM payroll_runs WHERE id=$1 AND status='draft'`, existingID); err != nil {
			return "", err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	var runID string
	err = tx.QueryRow(ctx, `INSERT INTO payroll_runs(org_id,month,created_by) VALUES($1,$2,$3) RETURNING id`, orgID, month, actorID).Scan(&runID)
	if err != nil {
		return "", err
	}
	rules, err := loadPayrollRules(ctx, tx, orgID)
	if err != nil {
		return "", err
	}
	start := month
	end := month.AddDate(0, 1, 0)
	rows, err := tx.Query(ctx, `SELECT id, base_salary_cents, currency FROM users WHERE org_id=$1 AND active=true ORDER BY full_name`, orgID)
	if err != nil {
		return "", err
	}
	type payrollEmployee struct {
		userID   string
		base     int64
		currency string
	}
	employees := make([]payrollEmployee, 0, 256)
	for rows.Next() {
		var emp payrollEmployee
		if err := rows.Scan(&emp.userID, &emp.base, &emp.currency); err != nil {
			rows.Close()
			return "", err
		}
		employees = append(employees, emp)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return "", err
	}
	rows.Close()
	var totalGross, totalDeduct, totalEmployer, totalNet int64
	for _, emp := range employees {
		userID, base, currency := emp.userID, emp.base, emp.currency
		stats, err := payrollAttendanceStats(ctx, tx, orgID, userID, start, end)
		if err != nil {
			return "", err
		}
		overtimeApproved, err := payrollApprovedOvertime(ctx, tx, orgID, userID, start, end)
		if err != nil {
			return "", err
		}
		unpaidLeaveDays, err := payrollUnpaidLeaveDays(ctx, tx, orgID, userID, start, end)
		if err != nil {
			return "", err
		}
		ewaDeduction, err := payrollApprovedEWACents(ctx, tx, orgID, userID, start, end)
		if err != nil {
			return "", err
		}
		workHours := rule(rules, "workday_hours", 8)
		otMultiplier := rule(rules, "ot_multiplier", 1.5)
		hourly := float64(base) / (22 * workHours)
		daily := float64(base) / 22
		overtimeCents := int64(math.Round(hourly * otMultiplier * float64(overtimeApproved) / 60))
		lateDeduction := int64(math.Round(hourly * float64(stats.LateMinutes) / 60))
		unpaidDeduction := int64(math.Round(daily * float64(unpaidLeaveDays)))
		gross := base + overtimeCents
		tax := calculateSalaryTaxCents(gross, currency, rules)
		nssfEmployee := int64(math.Round(float64(gross) * (rule(rules, "nssf_healthcare_employee_rate", 0) + rule(rules, "nssf_pension_employee_rate", 0))))
		nssfEmployer := int64(math.Round(float64(gross) * (rule(rules, "nssf_orc_employer_rate", 0) + rule(rules, "nssf_healthcare_employer_rate", 0) + rule(rules, "nssf_pension_employer_rate", 0))))
		net := gross - lateDeduction - unpaidDeduction - tax - nssfEmployee - ewaDeduction
		if net < 0 {
			net = 0
		}
		details := map[string]any{"late_minutes": stats.LateMinutes, "worked_minutes": stats.WorkedMinutes, "attendance_overtime_minutes": stats.OvertimeMinutes, "approved_overtime_minutes": overtimeApproved, "unpaid_leave_days": unpaidLeaveDays, "ewa_deduction_cents": ewaDeduction, "currency": currency}
		b, _ := json.Marshal(details)
		_, err = tx.Exec(ctx, `INSERT INTO payroll_items(org_id,payroll_run_id,user_id,base_salary_cents,overtime_cents,late_deduction_cents,unpaid_leave_deduction_cents,ewa_deduction_cents,tax_cents,nssf_employee_cents,nssf_employer_cents,gross_cents,net_cents,details) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, orgID, runID, userID, base, overtimeCents, lateDeduction, unpaidDeduction, ewaDeduction, tax, nssfEmployee, nssfEmployer, gross, net, b)
		if err != nil {
			return "", err
		}
		totalGross += gross
		totalDeduct += lateDeduction + unpaidDeduction + tax + nssfEmployee + ewaDeduction
		totalEmployer += nssfEmployer
		totalNet += net
	}
	_, err = tx.Exec(ctx, `UPDATE payroll_runs SET gross_cents=$1,deductions_cents=$2,employer_cost_cents=$3,net_cents=$4 WHERE id=$5`, totalGross, totalDeduct, totalEmployer, totalNet, runID)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return runID, nil
}

type attendancePayrollStats struct{ WorkedMinutes, LateMinutes, OvertimeMinutes int }

func payrollAttendanceStats(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, orgID, userID string, from, to time.Time) (attendancePayrollStats, error) {
	var st attendancePayrollStats
	err := q.QueryRow(ctx, `SELECT COALESCE(sum(total_minutes),0)::int, COALESCE(sum(late_minutes),0)::int, COALESCE(sum(overtime_minutes),0)::int FROM attendance_sessions WHERE org_id=$1 AND user_id=$2 AND clock_in_at >= $3 AND clock_in_at < $4`, orgID, userID, from, to).Scan(&st.WorkedMinutes, &st.LateMinutes, &st.OvertimeMinutes)
	return st, err
}
func payrollApprovedOvertime(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, orgID, userID string, from, to time.Time) (int, error) {
	var mins int
	err := q.QueryRow(ctx, `SELECT COALESCE(sum(minutes),0)::int FROM overtime_requests WHERE org_id=$1 AND user_id=$2 AND status='approved' AND work_date >= $3 AND work_date < $4`, orgID, userID, from, to).Scan(&mins)
	return mins, err
}
func payrollUnpaidLeaveDays(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, orgID, userID string, from, to time.Time) (int, error) {
	var days int
	err := q.QueryRow(ctx, `SELECT COALESCE(sum((end_date - start_date)+1),0)::int FROM leave_requests WHERE org_id=$1 AND user_id=$2 AND status='approved' AND leave_type IN ('unpaid','unpaid_leave') AND start_date >= $3 AND start_date < $4`, orgID, userID, from, to).Scan(&days)
	return days, err
}

func payrollApprovedEWACents(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, orgID, userID string, from, to time.Time) (int64, error) {
	var amount int64
	err := q.QueryRow(ctx, `SELECT COALESCE(sum(amount_cents),0) FROM ewa_requests WHERE org_id=$1 AND user_id=$2 AND status='approved' AND requested_at >= $3 AND requested_at < $4`, orgID, userID, from, to).Scan(&amount)
	return amount, err
}

func (s *Server) listPayrollRuns(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT id,month,status::text,gross_cents,deductions_cents,employer_cost_cents,net_cents,created_at FROM payroll_runs WHERE org_id=$1 ORDER BY month DESC LIMIT 36`, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, st string
		var m, created time.Time
		var gross, deduct, employer, net int64
		if err := rows.Scan(&id, &m, &st, &gross, &deduct, &employer, &net, &created); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "month": m.Format("2006-01"), "status": st, "gross_cents": gross, "deductions_cents": deduct, "employer_cost_cents": employer, "net_cents": net, "created_at": created})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "runs": items})
}

func (s *Server) getPayrollRun(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	id := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var run map[string]any
	var month, created time.Time
	var status string
	var gross, deduct, employer, net int64
	err := s.db.QueryRow(ctx, `SELECT month,status::text,gross_cents,deductions_cents,employer_cost_cents,net_cents,created_at FROM payroll_runs WHERE id=$1 AND org_id=$2`, id, claims.OrgID).Scan(&month, &status, &gross, &deduct, &employer, &net, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "payroll run not found")
		return
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	run = map[string]any{"id": id, "month": month.Format("2006-01"), "status": status, "gross_cents": gross, "deductions_cents": deduct, "employer_cost_cents": employer, "net_cents": net, "created_at": created}
	rows, err := s.db.Query(ctx, `SELECT p.id,p.user_id,u.full_name,p.base_salary_cents,p.overtime_cents,p.late_deduction_cents,p.unpaid_leave_deduction_cents,COALESCE(p.ewa_deduction_cents,0),p.tax_cents,p.nssf_employee_cents,p.nssf_employer_cents,p.gross_cents,p.net_cents,p.details FROM payroll_items p JOIN users u ON u.id=p.user_id WHERE p.payroll_run_id=$1 ORDER BY u.full_name`, id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var itemID, uid, name string
		var base, ot, late, unpaid, ewa, tax, nssfE, nssfEr, grossI, netI int64
		var details []byte
		if err := rows.Scan(&itemID, &uid, &name, &base, &ot, &late, &unpaid, &ewa, &tax, &nssfE, &nssfEr, &grossI, &netI, &details); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		var d map[string]any
		_ = json.Unmarshal(details, &d)
		items = append(items, map[string]any{"id": itemID, "user_id": uid, "full_name": name, "base_salary_cents": base, "overtime_cents": ot, "late_deduction_cents": late, "unpaid_leave_deduction_cents": unpaid, "ewa_deduction_cents": ewa, "tax_cents": tax, "nssf_employee_cents": nssfE, "nssf_employer_cents": nssfEr, "gross_cents": grossI, "net_cents": netI, "details": d})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "run": run, "items": items})
}

func (s *Server) approvePayrollRun(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	id := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	cmd, err := s.db.Exec(ctx, `UPDATE payroll_runs SET status='approved', approved_by=$1, approved_at=now() WHERE id=$2 AND org_id=$3 AND status='draft'`, claims.UserID, id, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if cmd.RowsAffected() == 0 {
		writeError(w, 404, "draft payroll run not found")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) payrollPayoutExport(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	id := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT u.full_name,u.email,u.phone,p.net_cents,u.currency,p.user_id FROM payroll_items p JOIN payroll_runs pr ON pr.id=p.payroll_run_id JOIN users u ON u.id=p.user_id WHERE pr.id=$1 AND pr.org_id=$2 ORDER BY u.full_name`, id, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var name, email string
		var phone *string
		var net int64
		var cur, uid string
		if err := rows.Scan(&name, &email, &phone, &net, &cur, &uid); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"employee_id": uid, "name": name, "email": email, "phone": phone, "amount_cents": net, "currency": cur, "status": "ready_for_bank_mapping"})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "bank_payout_export": items, "note": "Map employee bank accounts and send to your bank integration layer after approval."})
}

func (s *Server) faceDeviceEvent(w http.ResponseWriter, r *http.Request) {
	providedSecret := firstNonEmpty(r.Header.Get("X-Device-Webhook-Secret"), r.Header.Get("X-Device-Secret"))
	if s.cfg.DeviceWebhookSecret != "" && subtle.ConstantTimeCompare([]byte(providedSecret), []byte(s.cfg.DeviceWebhookSecret)) != 1 {
		writeError(w, 401, "invalid device secret")
		return
	}
	var req struct {
		OrgID           string   `json:"org_id"`
		UserID          string   `json:"user_id"`
		EmployeeCode    string   `json:"employee_code"`
		DeviceSN        string   `json:"device_sn"`
		EventType       string   `json:"event_type"`
		EventAt         string   `json:"event_at"`
		ExternalEventID string   `json:"external_event_id"`
		FaceScore       *float64 `json:"face_score"`
		Lat             *float64 `json:"lat"`
		Lng             *float64 `json:"lng"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.OrgID = strings.TrimSpace(req.OrgID)
	req.UserID = strings.TrimSpace(req.UserID)
	req.EmployeeCode = strings.TrimSpace(req.EmployeeCode)
	req.DeviceSN = strings.TrimSpace(req.DeviceSN)
	req.ExternalEventID = strings.TrimSpace(req.ExternalEventID)
	if req.OrgID == "" || req.DeviceSN == "" || (req.UserID == "" && req.EmployeeCode == "") {
		writeError(w, 400, "org_id, device_sn, and either user_id or employee_code are required")
		return
	}
	if !validOptionalLatLng(req.Lat, req.Lng) {
		writeError(w, 400, "lat/lng must both be provided and valid")
		return
	}
	if req.FaceScore != nil && (*req.FaceScore < 0 || *req.FaceScore > 100) {
		writeError(w, 400, "face_score must be between 0 and 100")
		return
	}
	eventType := strings.ToLower(strings.TrimSpace(req.EventType))
	if eventType == "" {
		eventType = "in"
	}
	if eventType != "in" && eventType != "out" {
		writeError(w, 400, "event_type must be in or out")
		return
	}
	at := time.Now().UTC()
	if req.EventAt != "" {
		p, err := time.Parse(time.RFC3339, req.EventAt)
		if err != nil {
			writeError(w, 400, "event_at must be RFC3339, for example 2026-06-06T09:00:00Z")
			return
		}
		at = p.UTC()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer tx.Rollback(ctx)
	if req.UserID == "" {
		err = tx.QueryRow(ctx, `SELECT id FROM users WHERE org_id=$1 AND employee_code=$2 AND active=true`, req.OrgID, req.EmployeeCode).Scan(&req.UserID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 404, "employee_code not found")
			return
		}
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
	} else {
		var active bool
		err = tx.QueryRow(ctx, `SELECT active FROM users WHERE org_id=$1 AND id=$2`, req.OrgID, req.UserID).Scan(&active)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 404, "user_id not found")
			return
		}
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		if !active {
			writeError(w, 400, "user is inactive")
			return
		}
	}
	if req.ExternalEventID != "" {
		var existingID string
		err = tx.QueryRow(ctx, `SELECT id FROM device_events WHERE org_id=$1 AND device_sn=$2 AND external_event_id=$3`, req.OrgID, req.DeviceSN, req.ExternalEventID).Scan(&existingID)
		if err == nil {
			writeJSON(w, 200, map[string]any{"ok": true, "duplicate": true, "device_event_id": existingID})
			return
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 500, err.Error())
			return
		}
	}
	raw, _ := json.Marshal(req)
	fraudStatus := "normal"
	fraudScore := 0
	fraudReasons := []string{"no fraud signal detected"}
	if req.FaceScore == nil {
		fraudStatus, fraudScore, fraudReasons = "needs_review", 45, []string{"face device event missing face_score"}
	} else if *req.FaceScore < 70 {
		fraudStatus, fraudScore, fraudReasons = "needs_review", 70, []string{fmt.Sprintf("low face device score %.2f", *req.FaceScore)}
	} else if *req.FaceScore < 80 {
		fraudStatus, fraudScore, fraudReasons = "warning", 30, []string{fmt.Sprintf("borderline face device score %.2f", *req.FaceScore)}
	}
	var devID, eventID string
	err = tx.QueryRow(ctx, `INSERT INTO device_events(org_id,user_id,device_sn,event_type,event_at,external_event_id,face_score,raw_payload) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8) RETURNING id`, req.OrgID, req.UserID, req.DeviceSN, eventType, at, req.ExternalEventID, req.FaceScore, raw).Scan(&devID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	err = tx.QueryRow(ctx, `INSERT INTO attendance_events(org_id,user_id,kind,event_at,lat,lng,source,device_sn,face_score,fraud_status,fraud_score,fraud_reasons) VALUES($1,$2,$3,$4,$5,$6,'face_device',$7,$8,$9,$10,$11) RETURNING id`, req.OrgID, req.UserID, eventType, at, req.Lat, req.Lng, req.DeviceSN, req.FaceScore, fraudStatus, fraudScore, fraudReasons).Scan(&eventID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if eventType == "in" {
		var openID string
		err = tx.QueryRow(ctx, `SELECT id FROM attendance_sessions WHERE org_id=$1 AND user_id=$2 AND clock_out_at IS NULL ORDER BY clock_in_at DESC LIMIT 1 FOR UPDATE`, req.OrgID, req.UserID).Scan(&openID)
		if err == nil {
			writeError(w, 409, "device user already has an open attendance session")
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 500, err.Error())
			return
		}
		_, err = tx.Exec(ctx, `INSERT INTO attendance_sessions(org_id,user_id,clock_in_id,clock_in_at,late_minutes) VALUES($1,$2,$3,$4,$5)`, req.OrgID, req.UserID, eventID, at, lateMinutes(at))
	} else {
		var cmd pgconn.CommandTag
		cmd, err = tx.Exec(ctx, `UPDATE attendance_sessions SET clock_out_id=$1,clock_out_at=$2,total_minutes=GREATEST(0, EXTRACT(EPOCH FROM ($2-clock_in_at))::int/60),overtime_minutes=GREATEST(0,(EXTRACT(EPOCH FROM ($2-clock_in_at))::int/60)-480),updated_at=now() WHERE id=(SELECT id FROM attendance_sessions WHERE org_id=$3 AND user_id=$4 AND clock_out_at IS NULL ORDER BY clock_in_at DESC LIMIT 1 FOR UPDATE)`, eventID, at, req.OrgID, req.UserID)
		if err == nil && cmd.RowsAffected() == 0 {
			writeError(w, 409, "device user has no open clock-in session")
			return
		}
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if fraudStatus != "normal" {
		_ = s.writeAudit(ctx, tx, req.OrgID, req.UserID, "attendance.face_device_warning", "attendance_event", eventID, map[string]any{"score": fraudScore, "reasons": fraudReasons})
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	s.invalidateOrgCaches(req.OrgID)
	writeJSON(w, 201, map[string]any{"ok": true, "device_event_id": devID, "attendance_event_id": eventID, "fraud_status": fraudStatus, "fraud_score": fraudScore, "fraud_reasons": fraudReasons})
}

func (s *Server) reportSummary(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	period := strings.ToLower(valueOr(r.URL.Query().Get("period"), "daily"))
	base := queryDate(r, "date", time.Now())
	from, to := periodBounds(period, base)
	cacheKey := fmt.Sprintf("report_summary:%s:%s:%s", claims.OrgID, period, from.Format("2006-01-02"))
	var cached map[string]any
	if s.cfg.CacheTTLSeconds > 0 && s.cache.GetJSON(cacheKey, &cached) {
		cached["cache_hit"] = true
		writeJSON(w, 200, cached)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var employees, clockedIn, late, openSessions, visits, fraudWarnings int
	err := s.db.QueryRow(ctx, `SELECT
		(SELECT count(*)::int FROM users WHERE org_id=$1 AND active=true),
		(SELECT count(DISTINCT user_id)::int FROM attendance_events WHERE org_id=$1 AND kind='in' AND event_at >= $2 AND event_at < $3),
		(SELECT count(*)::int FROM attendance_sessions WHERE org_id=$1 AND late_minutes > 0 AND clock_in_at >= $2 AND clock_in_at < $3),
		(SELECT count(*)::int FROM attendance_sessions WHERE org_id=$1 AND clock_out_at IS NULL),
		(SELECT count(*)::int FROM sales_visits WHERE org_id=$1 AND check_in_at >= $2 AND check_in_at < $3),
		(SELECT count(*)::int FROM attendance_events WHERE org_id=$1 AND fraud_status <> 'normal' AND event_at >= $2 AND event_at < $3)`, claims.OrgID, from, to).Scan(&employees, &clockedIn, &late, &openSessions, &visits, &fraudWarnings)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	payload := map[string]any{"ok": true, "period": period, "from": from, "to": to, "cache_hit": false, "summary": map[string]any{"employees": employees, "clocked_in": clockedIn, "absent_estimate": maxInt(0, employees-clockedIn), "late_sessions": late, "open_sessions": openSessions, "sales_visits": visits, "fraud_warnings": fraudWarnings}}
	if s.cfg.CacheTTLSeconds > 0 {
		s.cache.SetJSON(cacheKey, payload, time.Duration(s.cfg.CacheTTLSeconds)*time.Second)
	}
	writeJSON(w, 200, payload)
}

func (s *Server) reportInsights(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	period := strings.ToLower(valueOr(r.URL.Query().Get("period"), "monthly"))
	base := queryDate(r, "date", time.Now())
	from, to := periodBounds(period, base)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	insights := []string{}
	rows, err := s.db.Query(ctx, `SELECT u.full_name, COALESCE(sum(s.late_minutes),0)::int AS late_total FROM attendance_sessions s JOIN users u ON u.id=s.user_id WHERE s.org_id=$1 AND s.clock_in_at >= $2 AND s.clock_in_at < $3 GROUP BY u.full_name HAVING COALESCE(sum(s.late_minutes),0) > 0 ORDER BY late_total DESC LIMIT 5`, claims.OrgID, from, to)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			var mins int
			_ = rows.Scan(&name, &mins)
			insights = append(insights, fmt.Sprintf("%s has %d late minutes in this %s.", name, mins, period))
		}
	}
	var ot int
	_ = s.db.QueryRow(ctx, `SELECT COALESCE(sum(overtime_minutes),0)::int FROM attendance_sessions WHERE org_id=$1 AND clock_in_at >= $2 AND clock_in_at < $3`, claims.OrgID, from, to).Scan(&ot)
	if ot > 0 {
		insights = append(insights, fmt.Sprintf("Total detected overtime is %d minutes. Review overtime approvals before payroll.", ot))
	}
	var visits int
	_ = s.db.QueryRow(ctx, `SELECT count(*) FROM sales_visits WHERE org_id=$1 AND check_in_at >= $2 AND check_in_at < $3`, claims.OrgID, from, to).Scan(&visits)
	if visits == 0 {
		insights = append(insights, "No sales visits recorded in this period.")
	} else {
		insights = append(insights, fmt.Sprintf("Sales team recorded %d customer visits in this period.", visits))
	}
	if len(insights) == 0 {
		insights = append(insights, "No risk pattern found for this period.")
	}
	writeJSON(w, 200, map[string]any{"ok": true, "period": period, "from": from, "to": to, "insights": insights})
}

func (s *Server) sendDailyTelegramReport(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	day := queryDate(r, "date", time.Now())
	from, to := periodBounds("daily", day)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var employees, clockedIn, late, visits, fraudWarnings int
	err := s.db.QueryRow(ctx, `SELECT
		(SELECT count(*)::int FROM users WHERE org_id=$1 AND active=true),
		(SELECT count(DISTINCT user_id)::int FROM attendance_events WHERE org_id=$1 AND kind='in' AND event_at >= $2 AND event_at < $3),
		(SELECT count(*)::int FROM attendance_sessions WHERE org_id=$1 AND late_minutes > 0 AND clock_in_at >= $2 AND clock_in_at < $3),
		(SELECT count(*)::int FROM sales_visits WHERE org_id=$1 AND check_in_at >= $2 AND check_in_at < $3),
		(SELECT count(*)::int FROM attendance_events WHERE org_id=$1 AND fraud_status <> 'normal' AND event_at >= $2 AND event_at < $3)`, claims.OrgID, from, to).Scan(&employees, &clockedIn, &late, &visits, &fraudWarnings)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	text := fmt.Sprintf("📊 <b>CheckinMe Daily Report</b>\nDate: %s\nEmployees: %d\nClocked in: %d\nAbsent estimate: %d\nLate sessions: %d\nFraud warnings: %d\nSales visits: %d", day.Format("2006-01-02"), employees, clockedIn, maxInt(0, employees-clockedIn), late, fraudWarnings, visits)
	if err := s.telegram.SendMessage(ctx, s.orgTelegramChatID(ctx, claims.OrgID), text); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "sent": true})
}

func (s *Server) listDepartments(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT id,name,description,active,created_at FROM departments WHERE org_id=$1 ORDER BY name`, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name string
		var desc *string
		var active bool
		var created time.Time
		if err := rows.Scan(&id, &name, &desc, &active, &created); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "description": desc, "active": active, "created_at": created})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "departments": items})
}

func (s *Server) createDepartment(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, 400, "name is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var id string
	err := s.db.QueryRow(ctx, `INSERT INTO departments(org_id,name,description) VALUES($1,$2,$3) RETURNING id`, claims.OrgID, strings.TrimSpace(req.Name), nullIfEmpty(req.Description)).Scan(&id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "id": id})
}

func (s *Server) listShifts(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT id,name,start_time::text,end_time::text,break_minutes,grace_minutes,active,created_at FROM shifts WHERE org_id=$1 ORDER BY start_time,name`, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name, start, end string
		var breakMin, grace int
		var active bool
		var created time.Time
		if err := rows.Scan(&id, &name, &start, &end, &breakMin, &grace, &active, &created); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "start_time": start, "end_time": end, "break_minutes": breakMin, "grace_minutes": grace, "active": active, "created_at": created})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "shifts": items})
}

func (s *Server) createShift(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	var req struct {
		Name         string `json:"name"`
		StartTime    string `json:"start_time"`
		EndTime      string `json:"end_time"`
		BreakMinutes int    `json:"break_minutes"`
		GraceMinutes int    `json:"grace_minutes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || req.StartTime == "" || req.EndTime == "" {
		writeError(w, 400, "name, start_time and end_time are required")
		return
	}
	if _, err := time.Parse("15:04", req.StartTime); err != nil {
		writeError(w, 400, "start_time must use HH:MM")
		return
	}
	if _, err := time.Parse("15:04", req.EndTime); err != nil {
		writeError(w, 400, "end_time must use HH:MM")
		return
	}
	if req.BreakMinutes < 0 || req.GraceMinutes < 0 {
		writeError(w, 400, "break_minutes and grace_minutes cannot be negative")
		return
	}
	if req.GraceMinutes == 0 {
		req.GraceMinutes = 5
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var id string
	err := s.db.QueryRow(ctx, `INSERT INTO shifts(org_id,name,start_time,end_time,break_minutes,grace_minutes) VALUES($1,$2,$3::time,$4::time,$5,$6) RETURNING id`, claims.OrgID, req.Name, req.StartTime, req.EndTime, req.BreakMinutes, req.GraceMinutes).Scan(&id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "id": id})
}

func (s *Server) listScheduleAssignments(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT a.id,a.user_id,u.full_name,a.department_id,d.name,a.shift_id,sh.name,a.start_date,a.end_date,a.day_of_week,a.active FROM schedule_assignments a LEFT JOIN users u ON u.id=a.user_id LEFT JOIN departments d ON d.id=a.department_id JOIN shifts sh ON sh.id=a.shift_id WHERE a.org_id=$1 ORDER BY a.start_date DESC`, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, shiftID, shiftName string
		var userID, userName, deptID, deptName *string
		var start, end time.Time
		var dow *int
		var active bool
		if err := rows.Scan(&id, &userID, &userName, &deptID, &deptName, &shiftID, &shiftName, &start, &end, &dow, &active); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "user_id": userID, "user_name": userName, "department_id": deptID, "department_name": deptName, "shift_id": shiftID, "shift_name": shiftName, "start_date": start.Format("2006-01-02"), "end_date": end.Format("2006-01-02"), "day_of_week": dow, "active": active})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "assignments": items})
}

func (s *Server) createScheduleAssignment(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	var req struct {
		UserID       *string `json:"user_id"`
		DepartmentID *string `json:"department_id"`
		ShiftID      string  `json:"shift_id"`
		StartDate    string  `json:"start_date"`
		EndDate      string  `json:"end_date"`
		DayOfWeek    *int    `json:"day_of_week"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ShiftID == "" || req.StartDate == "" || req.EndDate == "" || (req.UserID == nil && req.DepartmentID == nil) {
		writeError(w, 400, "shift_id, start_date, end_date and user_id or department_id are required")
		return
	}
	startDate, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		writeError(w, 400, "invalid start_date")
		return
	}
	endDate, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		writeError(w, 400, "invalid end_date")
		return
	}
	if endDate.Before(startDate) {
		writeError(w, 400, "end_date must be after or equal to start_date")
		return
	}
	if req.DayOfWeek != nil && (*req.DayOfWeek < 0 || *req.DayOfWeek > 6) {
		writeError(w, 400, "day_of_week must be between 0 and 6")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var id string
	err = s.db.QueryRow(ctx, `INSERT INTO schedule_assignments(org_id,user_id,department_id,shift_id,start_date,end_date,day_of_week) VALUES($1,$2,$3,$4,$5::date,$6::date,$7) RETURNING id`, claims.OrgID, req.UserID, req.DepartmentID, req.ShiftID, startDate, endDate, req.DayOfWeek).Scan(&id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "id": id})
}

func (s *Server) createAttendanceQRToken(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	var req struct {
		BranchID       string `json:"branch_id"`
		Label          string `json:"label"`
		TTLMinutes     *int   `json:"ttl_minutes"`
		TTLHours       *int   `json:"ttl_hours"`
		ExpiresAt      string `json:"expires_at"`
		NoExpiry       bool   `json:"no_expiry"`
		Unlimited      bool   `json:"unlimited"`
		QRSizePX       int    `json:"qr_size_px"`
		RequireGPS     *bool  `json:"require_gps"`
		AllowedRadiusM *int   `json:"allowed_radius_m"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.BranchID == "" {
		writeError(w, 400, "branch_id is required")
		return
	}
	if req.AllowedRadiusM != nil && *req.AllowedRadiusM <= 0 {
		writeError(w, 400, "allowed_radius_m must be positive")
		return
	}
	if req.QRSizePX == 0 {
		req.QRSizePX = 512
	}
	if req.QRSizePX < 128 || req.QRSizePX > 1024 {
		writeError(w, 400, "qr_size_px must be between 128 and 1024")
		return
	}
	noExpiry := req.NoExpiry || req.Unlimited || (req.TTLMinutes != nil && *req.TTLMinutes == 0)
	if noExpiry && claims.Role != "owner" && claims.Role != "admin" {
		writeError(w, 403, "only owner or admin can create no-expiry QR tokens")
		return
	}
	now := time.Now().UTC()
	var expiresAt *time.Time
	if !noExpiry {
		if strings.TrimSpace(req.ExpiresAt) != "" {
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ExpiresAt))
			if err != nil {
				writeError(w, 400, "expires_at must be RFC3339, example: 2026-06-06T18:00:00+07:00")
				return
			}
			parsed = parsed.UTC()
			if !parsed.After(now) {
				writeError(w, 400, "expires_at must be in the future")
				return
			}
			expiresAt = &parsed
		} else {
			ttlMinutes := 480
			if req.TTLHours != nil {
				if *req.TTLHours <= 0 {
					writeError(w, 400, "ttl_hours must be positive, or use no_expiry=true")
					return
				}
				ttlMinutes = *req.TTLHours * 60
			} else if req.TTLMinutes != nil {
				if *req.TTLMinutes < 0 {
					writeError(w, 400, "ttl_minutes cannot be negative")
					return
				}
				if *req.TTLMinutes == 0 {
					// Already handled as no-expiry above. This guard is kept for clarity.
					writeError(w, 400, "ttl_minutes=0 requires owner/admin no-expiry mode")
					return
				}
				ttlMinutes = *req.TTLMinutes
			}
			expires := now.Add(time.Duration(ttlMinutes) * time.Minute)
			expiresAt = &expires
		}
	}
	requireGPS := true
	if req.RequireGPS != nil {
		requireGPS = *req.RequireGPS
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM branches WHERE org_id=$1 AND id=$2 AND active=true)`, claims.OrgID, req.BranchID).Scan(&exists); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if !exists {
		writeError(w, 404, "branch not found or inactive")
		return
	}
	var id, token string
	var expires pgtype.Timestamptz
	err := s.db.QueryRow(ctx, `INSERT INTO attendance_qr_tokens(org_id,branch_id,created_by,token,label,expires_at,require_gps,allowed_radius_m) VALUES($1,$2,$3,encode(gen_random_bytes(24),'hex'),$4,$5,$6,$7) RETURNING id,token,expires_at`, claims.OrgID, req.BranchID, claims.UserID, nullIfEmpty(req.Label), expiresAt, requireGPS, req.AllowedRadiusM).Scan(&id, &token, &expires)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	qrPNG, err := qrcode.Encode(token, qrcode.Medium, req.QRSizePX)
	if err != nil {
		writeError(w, 500, "failed to generate QR image")
		return
	}
	qrBase64 := base64.StdEncoding.EncodeToString(qrPNG)
	var expiresValue any
	if expires.Valid {
		expiresValue = expires.Time
	}
	writeJSON(w, 201, map[string]any{
		"ok":                  true,
		"id":                  id,
		"token":               token,
		"qr_content":          token,
		"qr_image_base64":     qrBase64,
		"qr_image_data_url":   "data:image/png;base64," + qrBase64,
		"qr_image_mime_type":  "image/png",
		"qr_image_size_px":    req.QRSizePX,
		"expires_at":          expiresValue,
		"no_expiry":           !expires.Valid,
		"require_gps":         requireGPS,
		"allowed_radius_m":    req.AllowedRadiusM,
		"clock_payload":       map[string]any{"source": "qr", "qr_token": token, "kind": "in"},
		"mobile_scan_payload": map[string]any{"source": "qr", "qr_token": token},
	})
}

func (s *Server) verifyAttendanceEvidence(ctx context.Context, q queryer, orgID, userID string, req attendanceClockRequest) (string, *string, *string, int, string) {
	source := strings.ToLower(strings.TrimSpace(req.Source))
	if req.QRToken != "" {
		source = "qr"
	} else if req.FaceScore != nil && source == "" {
		source = "face_scan"
	} else if req.Lat != nil && req.Lng != nil && source == "" {
		source = "gps"
	} else if source == "" {
		source = "mobile"
	}
	if source != "mobile" && source != "gps" && source != "qr" && source != "face_scan" {
		return source, nil, nil, 400, "invalid attendance source"
	}
	var branchID *string
	var qrTokenID *string
	if source == "face_scan" && req.FaceScore == nil {
		return source, branchID, qrTokenID, 400, "face_score is required for face_scan source"
	}
	if source == "face_scan" && req.FaceScore != nil && *req.FaceScore < 70 {
		return source, branchID, qrTokenID, 400, "face_score is below required threshold"
	}
	if source == "qr" {
		var tokenID string
		var bID string
		var expires pgtype.Timestamptz
		var requireGPS bool
		var radius *int
		err := q.QueryRow(ctx, `SELECT id,branch_id,expires_at,require_gps,allowed_radius_m FROM attendance_qr_tokens WHERE org_id=$1 AND token=$2 AND active=true`, orgID, req.QRToken).Scan(&tokenID, &bID, &expires, &requireGPS, &radius)
		if errors.Is(err, pgx.ErrNoRows) {
			return source, branchID, qrTokenID, 400, "invalid or inactive QR token"
		}
		if err != nil {
			return source, branchID, qrTokenID, 500, err.Error()
		}
		if expires.Valid && time.Now().UTC().After(expires.Time) {
			return source, branchID, qrTokenID, 400, "QR token expired"
		}
		branchID = &bID
		qrTokenID = &tokenID
		if requireGPS {
			if req.Lat == nil || req.Lng == nil {
				return source, branchID, qrTokenID, 400, "lat and lng are required for this QR token"
			}
			if ok, errText := validateBranchGeofence(ctx, q, orgID, bID, *req.Lat, *req.Lng, radius); !ok {
				return source, branchID, qrTokenID, 400, errText
			}
		}
		_, _ = q.Exec(ctx, `UPDATE attendance_qr_tokens SET scan_count=scan_count+1 WHERE id=$1`, tokenID)
		return source, branchID, qrTokenID, 0, ""
	}
	if source == "gps" || (req.Lat != nil && req.Lng != nil) {
		var bID *string
		err := q.QueryRow(ctx, `SELECT branch_id FROM users WHERE org_id=$1 AND id=$2`, orgID, userID).Scan(&bID)
		if err != nil {
			return source, branchID, qrTokenID, 500, err.Error()
		}
		if bID != nil && req.Lat != nil && req.Lng != nil {
			if ok, errText := validateBranchGeofence(ctx, q, orgID, *bID, *req.Lat, *req.Lng, nil); !ok {
				return source, branchID, qrTokenID, 400, errText
			}
			branchID = bID
		}
	}
	return source, branchID, qrTokenID, 0, ""
}

func (s *Server) assessAttendanceFraud(ctx context.Context, q queryer, orgID, userID, kind, source string, req attendanceClockRequest, branchID *string, qrTokenID *string, now time.Time) (attendanceFraudAssessment, error) {
	result := attendanceFraudAssessment{Status: "normal", Reasons: []string{}}
	add := func(score int, reason string) {
		if score <= 0 || strings.TrimSpace(reason) == "" {
			return
		}
		result.Score += score
		result.Reasons = append(result.Reasons, reason)
	}
	if req.MockLocation != nil && *req.MockLocation {
		add(100, "device reported mock/fake GPS location")
	}
	if req.GPSAccuracyM != nil && s.cfg.FraudMaxGPSAccuracyM > 0 && *req.GPSAccuracyM > s.cfg.FraudMaxGPSAccuracyM {
		add(25, fmt.Sprintf("low GPS accuracy: %dm exceeds %dm", *req.GPSAccuracyM, s.cfg.FraudMaxGPSAccuracyM))
	}
	if (source == "gps" || source == "qr") && (req.Lat == nil || req.Lng == nil) {
		add(45, "GPS/QR attendance missing location coordinates")
	}
	if source == "mobile" && req.Lat == nil && req.Lng == nil {
		add(15, "mobile attendance has no GPS evidence")
	}
	if source == "face_scan" && req.FaceScore != nil && *req.FaceScore < 80 {
		add(30, fmt.Sprintf("borderline face score %.2f", *req.FaceScore))
	}
	if qrTokenID != nil {
		var replayCount int
		seconds := s.cfg.FraudDuplicateSeconds
		if seconds <= 0 {
			seconds = 120
		}
		_ = q.QueryRow(ctx, `SELECT count(*)::int FROM attendance_events WHERE org_id=$1 AND user_id=$2 AND qr_token_id=$3 AND event_at >= $4`, orgID, userID, *qrTokenID, now.Add(-time.Duration(seconds)*time.Second)).Scan(&replayCount)
		if replayCount > 0 {
			add(35, fmt.Sprintf("QR token reused by same employee within %d seconds", seconds))
		}
	}
	var duplicateCount int
	seconds := s.cfg.FraudDuplicateSeconds
	if seconds <= 0 {
		seconds = 120
	}
	_ = q.QueryRow(ctx, `SELECT count(*)::int FROM attendance_events WHERE org_id=$1 AND user_id=$2 AND kind=$3 AND event_at >= $4`, orgID, userID, kind, now.Add(-time.Duration(seconds)*time.Second)).Scan(&duplicateCount)
	if duplicateCount > 0 {
		add(25, fmt.Sprintf("duplicate %s attendance event within %d seconds", kind, seconds))
	}
	if req.Lat != nil && req.Lng != nil {
		var prevAt time.Time
		var prevLat, prevLng float64
		err := q.QueryRow(ctx, `SELECT event_at, lat::float8, lng::float8 FROM attendance_events WHERE org_id=$1 AND user_id=$2 AND lat IS NOT NULL AND lng IS NOT NULL ORDER BY event_at DESC LIMIT 1`, orgID, userID).Scan(&prevAt, &prevLat, &prevLng)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return result, err
		}
		if err == nil {
			deltaSeconds := now.Sub(prevAt).Seconds()
			if deltaSeconds > 30 {
				distance := int(math.Round(haversineMeters(prevLat, prevLng, *req.Lat, *req.Lng)))
				speed := (float64(distance) / 1000) / (deltaSeconds / 3600)
				result.DistanceM = &distance
				result.TravelSpeedKPH = &speed
				if speed > s.cfg.FraudMaxSpeedKPH*2 {
					add(100, fmt.Sprintf("impossible travel speed %.1f km/h", speed))
				} else if speed > s.cfg.FraudMaxSpeedKPH {
					add(60, fmt.Sprintf("suspicious travel speed %.1f km/h", speed))
				}
			}
		}
	}
	if len(result.Reasons) == 0 {
		result.Reasons = []string{"no fraud signal detected"}
	}
	if result.Score >= s.cfg.FraudBlockScore {
		result.Status = "blocked"
		result.Blocked = true
		result.ReviewRequired = true
	} else if result.Score >= s.cfg.FraudWarnScore {
		result.Status = "needs_review"
		result.ReviewRequired = true
	} else if result.Score > 0 {
		result.Status = "warning"
		result.ReviewRequired = result.Score >= s.cfg.FraudWarnScore/2
	}
	return result, nil
}

func validateBranchGeofence(ctx context.Context, q queryer, orgID, branchID string, lat, lng float64, radiusOverride *int) (bool, string) {
	var bLat, bLng *float64
	var radius int
	err := q.QueryRow(ctx, `SELECT lat::float8,lng::float8,gps_radius_m FROM branches WHERE org_id=$1 AND id=$2 AND active=true`, orgID, branchID).Scan(&bLat, &bLng, &radius)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "branch not found or inactive"
	}
	if err != nil {
		return false, err.Error()
	}
	if bLat == nil || bLng == nil {
		return true, ""
	}
	if radiusOverride != nil && *radiusOverride > 0 {
		radius = *radiusOverride
	}
	distance := int(math.Round(haversineMeters(lat, lng, *bLat, *bLng)))
	if distance > radius {
		return false, fmt.Sprintf("outside allowed geofence: %dm away, allowed radius %dm", distance, radius)
	}
	return true, ""
}

func (s *Server) salesDailySummary(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	day := queryDate(r, "date", time.Now())
	from, to := periodBounds("daily", day)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT v.id,u.full_name,c.name,v.check_in_at,v.check_out_at,v.lat::float8,v.lng::float8,v.distance_m,v.notes FROM sales_visits v JOIN users u ON u.id=v.user_id JOIN customers c ON c.id=v.customer_id WHERE v.org_id=$1 AND v.check_in_at >= $2 AND v.check_in_at < $3 ORDER BY u.full_name,v.check_in_at`, claims.OrgID, from, to)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	visits := []map[string]any{}
	perEmployee := map[string]int{}
	for rows.Next() {
		var id, employee, customer string
		var checkIn time.Time
		var checkOut *time.Time
		var lat, lng *float64
		var dist *int
		var notes *string
		if err := rows.Scan(&id, &employee, &customer, &checkIn, &checkOut, &lat, &lng, &dist, &notes); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		perEmployee[employee]++
		visits = append(visits, map[string]any{"id": id, "employee": employee, "customer": customer, "check_in_at": checkIn, "check_out_at": checkOut, "lat": lat, "lng": lng, "distance_m": dist, "notes": notes})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "date": day.Format("2006-01-02"), "total_visits": len(visits), "per_employee": perEmployee, "route": visits})
}

func (s *Server) sendSalesDailyTelegramSummary(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	day := queryDate(r, "date", time.Now())
	from, to := periodBounds("daily", day)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT u.full_name,count(v.id)::int FROM sales_visits v JOIN users u ON u.id=v.user_id WHERE v.org_id=$1 AND v.check_in_at >= $2 AND v.check_in_at < $3 GROUP BY u.full_name ORDER BY count(v.id) DESC,u.full_name`, claims.OrgID, from, to)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	lines := []string{fmt.Sprintf("🗺️ <b>Sales Field Summary</b>\nDate: %s", day.Format("2006-01-02"))}
	total := 0
	for rows.Next() {
		var name string
		var count int
		_ = rows.Scan(&name, &count)
		total += count
		lines = append(lines, fmt.Sprintf("• %s: %d visits", name, count))
	}
	if total == 0 {
		lines = append(lines, "No customer visits recorded.")
	}
	if err := s.telegram.SendMessage(ctx, s.orgTelegramChatID(ctx, claims.OrgID), strings.Join(lines, "\n")); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "sent": true, "total_visits": total})
}

func (s *Server) payrollRunCSVExport(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	id := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT u.employee_code,u.full_name,u.email,p.base_salary_cents,p.overtime_cents,p.bonus_cents,p.late_deduction_cents,p.unpaid_leave_deduction_cents,COALESCE(p.ewa_deduction_cents,0),p.tax_cents,p.nssf_employee_cents,p.nssf_employer_cents,p.gross_cents,p.net_cents,u.currency FROM payroll_items p JOIN payroll_runs pr ON pr.id=p.payroll_run_id JOIN users u ON u.id=p.user_id WHERE pr.id=$1 AND pr.org_id=$2 ORDER BY u.full_name`, id, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	records := [][]string{{"employee_code", "full_name", "email", "base_salary_cents", "overtime_cents", "bonus_cents", "late_deduction_cents", "unpaid_leave_deduction_cents", "ewa_deduction_cents", "tax_cents", "nssf_employee_cents", "nssf_employer_cents", "gross_cents", "net_cents", "currency"}}
	for rows.Next() {
		var code *string
		var name, email, currency string
		vals := make([]int64, 11)
		if err := rows.Scan(&code, &name, &email, &vals[0], &vals[1], &vals[2], &vals[3], &vals[4], &vals[5], &vals[6], &vals[7], &vals[8], &vals[9], &vals[10], &currency); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		records = append(records, []string{ptrString(code), name, email, i64(vals[0]), i64(vals[1]), i64(vals[2]), i64(vals[3]), i64(vals[4]), i64(vals[5]), i64(vals[6]), i64(vals[7]), i64(vals[8]), i64(vals[9]), i64(vals[10]), currency})
	}
	writeCSV(w, "payroll-run.csv", records)
}

func (s *Server) payrollBankStatementCSVExport(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	id := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT u.employee_code,u.full_name,ba.bank_name,ba.account_name,ba.account_number,p.net_cents,u.currency FROM payroll_items p JOIN payroll_runs pr ON pr.id=p.payroll_run_id JOIN users u ON u.id=p.user_id LEFT JOIN bank_accounts ba ON ba.user_id=u.id AND ba.is_primary=true AND ba.active=true WHERE pr.id=$1 AND pr.org_id=$2 ORDER BY u.full_name`, id, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	records := [][]string{{"employee_code", "full_name", "bank_name", "account_name", "account_number", "amount_cents", "currency"}}
	for rows.Next() {
		var code, bank, accName, accNo *string
		var name, currency string
		var amount int64
		if err := rows.Scan(&code, &name, &bank, &accName, &accNo, &amount, &currency); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		records = append(records, []string{ptrString(code), name, ptrString(bank), ptrString(accName), ptrString(accNo), i64(amount), currency})
	}
	writeCSV(w, "bank-statement.csv", records)
}

func (s *Server) getDigitalPayslip(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	runID := chi.URLParam(r, "id")
	userID := chi.URLParam(r, "user_id")
	if userID != claims.UserID && claims.Role != "owner" && claims.Role != "admin" && claims.Role != "manager" {
		writeError(w, 403, "you can only view your own payslip")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var item struct {
		Name, Email, Currency, Month                              string
		Base, OT, Bonus, Late, Unpaid, EWA, Tax, NSSF, Gross, Net int64
	}
	var month time.Time
	err := s.db.QueryRow(ctx, `SELECT u.full_name,u.email,u.currency,pr.month,p.base_salary_cents,p.overtime_cents,p.bonus_cents,p.late_deduction_cents,p.unpaid_leave_deduction_cents,COALESCE(p.ewa_deduction_cents,0),p.tax_cents,p.nssf_employee_cents,p.gross_cents,p.net_cents FROM payroll_items p JOIN payroll_runs pr ON pr.id=p.payroll_run_id JOIN users u ON u.id=p.user_id WHERE pr.id=$1 AND pr.org_id=$2 AND p.user_id=$3`, runID, claims.OrgID, userID).Scan(&item.Name, &item.Email, &item.Currency, &month, &item.Base, &item.OT, &item.Bonus, &item.Late, &item.Unpaid, &item.EWA, &item.Tax, &item.NSSF, &item.Gross, &item.Net)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "payslip not found")
		return
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	item.Month = month.Format("2006-01")
	writeJSON(w, 200, map[string]any{"ok": true, "payslip": item})
}

func (s *Server) listBankAccounts(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	where := `WHERE ba.org_id=$1`
	args := []any{claims.OrgID}
	if claims.Role != "owner" && claims.Role != "admin" && claims.Role != "manager" {
		where += ` AND ba.user_id=$2`
		args = append(args, claims.UserID)
	}
	rows, err := s.db.Query(ctx, `SELECT ba.id,ba.user_id,u.full_name,ba.bank_name,ba.account_name,ba.account_number,ba.currency,ba.is_primary,ba.active,ba.created_at FROM bank_accounts ba JOIN users u ON u.id=ba.user_id `+where+` ORDER BY u.full_name,ba.created_at DESC`, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, uid, name, bank, accName, accNo, currency string
		var primary, active bool
		var created time.Time
		if err := rows.Scan(&id, &uid, &name, &bank, &accName, &accNo, &currency, &primary, &active, &created); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "user_id": uid, "full_name": name, "bank_name": bank, "account_name": accName, "account_number": accNo, "currency": currency, "is_primary": primary, "active": active, "created_at": created})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "bank_accounts": items})
}

func (s *Server) createBankAccount(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	var req struct {
		UserID        string `json:"user_id"`
		BankName      string `json:"bank_name"`
		AccountName   string `json:"account_name"`
		AccountNumber string `json:"account_number"`
		Currency      string `json:"currency"`
		IsPrimary     *bool  `json:"is_primary"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.UserID == "" {
		req.UserID = claims.UserID
	}
	if req.UserID != claims.UserID && claims.Role != "owner" && claims.Role != "admin" && claims.Role != "manager" {
		writeError(w, 403, "you can only add your own bank account")
		return
	}
	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	if req.Currency == "" {
		req.Currency = "USD"
	}
	if len(req.Currency) != 3 {
		writeError(w, 400, "currency must be a 3-letter ISO code")
		return
	}
	isPrimary := true
	if req.IsPrimary != nil {
		isPrimary = *req.IsPrimary
	}
	if strings.TrimSpace(req.BankName) == "" || strings.TrimSpace(req.AccountName) == "" || strings.TrimSpace(req.AccountNumber) == "" {
		writeError(w, 400, "bank_name, account_name and account_number are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer tx.Rollback(ctx)
	if isPrimary {
		_, _ = tx.Exec(ctx, `UPDATE bank_accounts SET is_primary=false WHERE org_id=$1 AND user_id=$2`, claims.OrgID, req.UserID)
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO bank_accounts(org_id,user_id,bank_name,account_name,account_number,currency,is_primary) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(org_id,user_id,account_number) DO UPDATE SET bank_name=EXCLUDED.bank_name,account_name=EXCLUDED.account_name,currency=EXCLUDED.currency,is_primary=EXCLUDED.is_primary,active=true RETURNING id`, claims.OrgID, req.UserID, strings.TrimSpace(req.BankName), strings.TrimSpace(req.AccountName), strings.TrimSpace(req.AccountNumber), strings.ToUpper(req.Currency), isPrimary).Scan(&id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "id": id})
}

func (s *Server) createBankTransferBatch(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	runID := chi.URLParam(r, "id")
	var req struct {
		Provider string `json:"provider"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Provider == "" {
		req.Provider = "manual_csv"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer tx.Rollback(ctx)
	var status string
	var count int
	var total int64
	err = tx.QueryRow(ctx, `SELECT status::text FROM payroll_runs WHERE id=$1 AND org_id=$2`, runID, claims.OrgID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "payroll run not found")
		return
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if status != "approved" {
		writeError(w, 409, "payroll run must be approved before creating bank batch")
		return
	}
	err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(sum(net_cents),0) FROM payroll_items WHERE payroll_run_id=$1`, runID).Scan(&count, &total)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	var batchID string
	err = tx.QueryRow(ctx, `INSERT INTO bank_transfer_batches(org_id,payroll_run_id,provider,status,total_items,total_cents,created_by) VALUES($1,$2,$3,'draft',$4,$5,$6) RETURNING id`, claims.OrgID, runID, req.Provider, count, total, claims.UserID).Scan(&batchID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	_, err = tx.Exec(ctx, `INSERT INTO bank_transfer_items(org_id,batch_id,user_id,bank_account_id,amount_cents,currency,status) SELECT $1,$2,p.user_id,ba.id,p.net_cents,u.currency,CASE WHEN ba.id IS NULL THEN 'missing_bank_account' ELSE 'ready' END FROM payroll_items p JOIN users u ON u.id=p.user_id LEFT JOIN bank_accounts ba ON ba.user_id=p.user_id AND ba.is_primary=true AND ba.active=true WHERE p.payroll_run_id=$3`, claims.OrgID, batchID, runID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "batch_id": batchID, "provider": req.Provider, "total_items": count, "total_cents": total, "note": "Draft batch created. Submit through your bank API adapter after adding bank credentials and bank-approved API spec."})
}

func (s *Server) listBankTransferBatches(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctx, `SELECT id,payroll_run_id,provider,status,total_items,total_cents,created_at,submitted_at FROM bank_transfer_batches WHERE org_id=$1 ORDER BY created_at DESC LIMIT 50`, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, runID, provider, status string
		var totalItems int
		var total int64
		var created time.Time
		var submitted *time.Time
		if err := rows.Scan(&id, &runID, &provider, &status, &totalItems, &total, &created, &submitted); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "payroll_run_id": runID, "provider": provider, "status": status, "total_items": totalItems, "total_cents": total, "created_at": created, "submitted_at": submitted})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "batches": items})
}

func (s *Server) markBankBatchSubmitted(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	id := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	cmd, err := s.db.Exec(ctx, `UPDATE bank_transfer_batches SET status='submitted',submitted_at=now() WHERE id=$1 AND org_id=$2 AND status='draft'`, id, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if cmd.RowsAffected() == 0 {
		writeError(w, 404, "draft bank batch not found")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) createEWARequest(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	var req struct {
		AmountCents int64  `json:"amount_cents"`
		Reason      string `json:"reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.AmountCents <= 0 {
		writeError(w, 400, "amount_cents must be greater than 0")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rules, _ := loadPayrollRules(ctx, s.db, claims.OrgID)
	maxPct := rule(rules, "ewa_max_percent_of_monthly_salary", 0.40)
	var base int64
	if err := s.db.QueryRow(ctx, `SELECT base_salary_cents FROM users WHERE org_id=$1 AND id=$2 AND active=true`, claims.OrgID, claims.UserID).Scan(&base); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	month := time.Date(time.Now().UTC().Year(), time.Now().UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	var already int64
	_ = s.db.QueryRow(ctx, `SELECT COALESCE(sum(amount_cents),0) FROM ewa_requests WHERE org_id=$1 AND user_id=$2 AND status IN ('pending','approved') AND requested_at >= $3 AND requested_at < $4`, claims.OrgID, claims.UserID, month, month.AddDate(0, 1, 0)).Scan(&already)
	available := int64(math.Round(float64(base)*maxPct)) - already
	if available < 0 {
		available = 0
	}
	if req.AmountCents > available {
		writeError(w, 400, fmt.Sprintf("requested amount exceeds available EWA balance: %d cents", available))
		return
	}
	var id string
	err := s.db.QueryRow(ctx, `INSERT INTO ewa_requests(org_id,user_id,amount_cents,currency,reason) SELECT org_id,id,$1,currency,$2 FROM users WHERE org_id=$3 AND id=$4 RETURNING id`, req.AmountCents, nullIfEmpty(req.Reason), claims.OrgID, claims.UserID).Scan(&id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "id": id, "available_after_request_cents": available - req.AmountCents})
}

func (s *Server) listEWARequests(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	where := `WHERE e.org_id=$1`
	args := []any{claims.OrgID}
	if claims.Role != "owner" && claims.Role != "admin" && claims.Role != "manager" {
		where += ` AND e.user_id=$2`
		args = append(args, claims.UserID)
	}
	rows, err := s.db.Query(ctx, `SELECT e.id,e.user_id,u.full_name,e.amount_cents,e.currency,e.reason,e.status::text,e.requested_at,e.reviewed_at,e.review_note FROM ewa_requests e JOIN users u ON u.id=e.user_id `+where+` ORDER BY e.requested_at DESC LIMIT 100`, args...)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, uid, name, currency, status string
		var reason, note *string
		var amount int64
		var requested time.Time
		var reviewed *time.Time
		if err := rows.Scan(&id, &uid, &name, &amount, &currency, &reason, &status, &requested, &reviewed, &note); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		items = append(items, map[string]any{"id": id, "user_id": uid, "full_name": name, "amount_cents": amount, "currency": currency, "reason": reason, "status": status, "requested_at": requested, "reviewed_at": reviewed, "review_note": note})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "ewa_requests": items})
}

func (s *Server) reviewEWARequest(w http.ResponseWriter, r *http.Request) {
	claims := middleware.Claims(r)
	id := chi.URLParam(r, "id")
	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	if req.Status != "approved" && req.Status != "rejected" {
		writeError(w, 400, "status must be approved or rejected")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	cmd, err := s.db.Exec(ctx, `UPDATE ewa_requests SET status=$1::approval_status,reviewed_by=$2,reviewed_at=now(),review_note=$3 WHERE id=$4 AND org_id=$5 AND status='pending'`, req.Status, claims.UserID, nullIfEmpty(req.Note), id, claims.OrgID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if cmd.RowsAffected() == 0 {
		writeError(w, 404, "pending EWA request not found")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (s *Server) requestTimeoutMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seconds := s.cfg.RequestTimeoutSeconds
		if seconds <= 0 {
			seconds = 15
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(seconds)*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.RateLimitEnabled || r.Method == http.MethodOptions || r.URL.Path == "/health" || r.URL.Path == "/ready" {
			next.ServeHTTP(w, r)
			return
		}
		rpm := s.cfg.RateLimitRPM
		if rpm <= 0 {
			rpm = 240
		}
		burst := s.cfg.RateLimitBurst
		if burst <= 0 {
			burst = maxInt(1, rpm/3)
		}
		key := clientIP(r)
		now := time.Now()
		fillPerSecond := float64(rpm) / 60.0

		s.rateMu.Lock()
		b := s.rateMap[key]
		if b == nil {
			b = &rateBucket{tokens: float64(burst), updated: now}
			s.rateMap[key] = b
		}
		elapsed := now.Sub(b.updated).Seconds()
		if elapsed > 0 {
			b.tokens = math.Min(float64(burst), b.tokens+elapsed*fillPerSecond)
			b.updated = now
		}
		allowed := b.tokens >= 1
		if allowed {
			b.tokens -= 1
		}
		// Opportunistic cleanup keeps the map bounded without a background goroutine.
		if len(s.rateMap) > 5000 {
			cutoff := now.Add(-10 * time.Minute)
			for k, v := range s.rateMap {
				if v.updated.Before(cutoff) {
					delete(s.rateMap, k)
				}
			}
		}
		s.rateMu.Unlock()

		if !allowed {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		dur := time.Since(start)
		threshold := time.Duration(s.cfg.SlowRequestMS) * time.Millisecond
		if s.cfg.SlowRequestMS <= 0 {
			threshold = 700 * time.Millisecond
		}
		if dur >= threshold || rec.status >= 500 {
			log.Printf("http request method=%s path=%s status=%d duration_ms=%d", r.Method, r.URL.Path, rec.status, dur.Milliseconds())
		}
	})
}

func (s *Server) runAsync(fn func()) {
	select {
	case s.asyncSem <- struct{}{}:
		go func() {
			defer func() { <-s.asyncSem }()
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("async job panic recovered: %v", rec)
				}
			}()
			fn()
		}()
	default:
		log.Printf("async worker saturated; background job skipped")
	}
}

func (s *Server) invalidateOrgCaches(orgID string) {
	s.cache.DeletePrefix("report_summary:" + orgID + ":")
	s.cache.DeletePrefix("sales_summary:" + orgID + ":")
	// Keep org_telegram cache because it is stable and harmless if stale for a short TTL.
}

func (s *Server) writeAudit(ctx context.Context, q queryer, orgID, actorID, action, entity, entityID string, meta any) error {
	buf, err := json.Marshal(meta)
	if err != nil {
		buf = []byte(`{}`)
	}
	_, err = q.Exec(ctx, `INSERT INTO audit_logs(org_id,actor_id,action,entity,entity_id,meta) VALUES($1,$2,$3,$4,$5,$6::jsonb)`, orgID, actorID, action, nullIfEmpty(entity), nullIfEmpty(entityID), string(buf))
	return err
}

func (s *Server) systemPerformance(w http.ResponseWriter, r *http.Request) {
	stat := s.db.Stat()
	s.rateMu.Lock()
	rateClients := len(s.rateMap)
	s.rateMu.Unlock()
	writeJSON(w, 200, map[string]any{
		"ok": true,
		"cache": map[string]any{
			"type":        "memory_ttl",
			"items":       s.cache.Size(),
			"ttl_seconds": s.cfg.CacheTTLSeconds,
		},
		"async": map[string]any{
			"limit":  cap(s.asyncSem),
			"in_use": len(s.asyncSem),
		},
		"rate_limit": map[string]any{
			"enabled":             s.cfg.RateLimitEnabled,
			"requests_per_minute": s.cfg.RateLimitRPM,
			"burst":               s.cfg.RateLimitBurst,
			"tracked_clients":     rateClients,
		},
		"postgres_pool": map[string]any{
			"acquire_count":          stat.AcquireCount(),
			"acquire_duration_ms":    stat.AcquireDuration().Milliseconds(),
			"acquired_conns":         stat.AcquiredConns(),
			"canceled_acquire_count": stat.CanceledAcquireCount(),
			"constructing_conns":     stat.ConstructingConns(),
			"empty_acquire_count":    stat.EmptyAcquireCount(),
			"idle_conns":             stat.IdleConns(),
			"max_conns":              stat.MaxConns(),
			"total_conns":            stat.TotalConns(),
		},
	})
}

// ---------- helpers ----------

type queryer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func seedPayrollDefaults(ctx context.Context, q queryer, orgID string) error {
	defaults := []struct {
		key  string
		val  float64
		desc string
	}{
		{"workday_hours", 8, "Standard work hours per day"},
		{"late_grace_minutes", 5, "Grace period before late deduction"},
		{"ot_multiplier", 1.5, "Overtime pay multiplier"},
		{"exchange_rate_usd_khr", 4100, "Editable FX rate for salary tax conversion"},
		{"nssf_orc_employer_rate", 0.008, "Occupational risk contribution employer rate"},
		{"nssf_healthcare_employee_rate", 0.013, "Healthcare contribution employee rate; verify before production"},
		{"nssf_healthcare_employer_rate", 0.013, "Healthcare contribution employer rate; verify before production"},
		{"nssf_pension_employee_rate", 0.020, "Pension contribution employee rate; verify before production"},
		{"nssf_pension_employer_rate", 0.020, "Pension contribution employer rate; verify before production"},
		{"ewa_max_percent_of_monthly_salary", 0.40, "Maximum earned wage access advance as a percentage of monthly base salary"},
	}
	for _, d := range defaults {
		if _, err := q.Exec(ctx, `INSERT INTO payroll_rules(org_id,rule_key,rule_value,description) VALUES($1,$2,$3,$4) ON CONFLICT(org_id,rule_key) DO NOTHING`, orgID, d.key, d.val, d.desc); err != nil {
			return err
		}
	}
	brackets := []struct {
		min, max int64
		rate     float64
		deduct   int64
	}{
		{0, 1500000, 0, 0},
		{1500001, 2000000, 0.05, 75000},
		{2000001, 8500000, 0.10, 175000},
		{8500001, 12500000, 0.15, 600000},
		{12500001, 0, 0.20, 1225000},
	}
	for _, b := range brackets {
		var maxVal any
		if b.max > 0 {
			maxVal = b.max
		}
		if _, err := q.Exec(ctx, `INSERT INTO payroll_tax_brackets(org_id,currency,min_amount,max_amount,rate,deduction) VALUES($1,'KHR',$2,$3,$4,$5) ON CONFLICT(org_id,currency,min_amount) DO UPDATE SET max_amount=EXCLUDED.max_amount,rate=EXCLUDED.rate,deduction=EXCLUDED.deduction,active=true`, orgID, b.min, maxVal, b.rate, b.deduct); err != nil {
			return err
		}
	}
	return nil
}

func loadPayrollRules(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, orgID string) (map[string]float64, error) {
	rows, err := q.Query(ctx, `SELECT rule_key, rule_value FROM payroll_rules WHERE org_id=$1 AND active=true`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]float64{}
	for rows.Next() {
		var k string
		var v float64
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, nil
}
func rule(m map[string]float64, k string, def float64) float64 {
	if v, ok := m[k]; ok {
		return v
	}
	return def
}

func calculateSalaryTaxCents(grossCents int64, currency string, rules map[string]float64) int64 {
	if grossCents <= 0 {
		return 0
	}
	fx := rule(rules, "exchange_rate_usd_khr", 4100)
	grossKHR := float64(grossCents) / 100
	if strings.ToUpper(currency) == "USD" {
		grossKHR = grossKHR * fx
	}
	var taxKHR float64
	switch {
	case grossKHR <= 1500000:
		taxKHR = 0
	case grossKHR <= 2000000:
		taxKHR = grossKHR*0.05 - 75000
	case grossKHR <= 8500000:
		taxKHR = grossKHR*0.10 - 175000
	case grossKHR <= 12500000:
		taxKHR = grossKHR*0.15 - 600000
	default:
		taxKHR = grossKHR*0.20 - 1225000
	}
	if taxKHR < 0 {
		return 0
	}
	if strings.ToUpper(currency) == "USD" {
		return int64(math.Round((taxKHR / fx) * 100))
	}
	return int64(math.Round(taxKHR * 100))
}

func lateMinutes(t time.Time) int {
	local := t.In(time.FixedZone("ICT", 7*3600))
	shift := time.Date(local.Year(), local.Month(), local.Day(), 9, 5, 0, 0, local.Location())
	if local.After(shift) {
		return int(local.Sub(shift).Minutes())
	}
	return 0
}
func haversineMeters(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371000
	dLat := (lat2 - lat1) * math.Pi / 180
	dLng := (lng2 - lng1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic recovered: %v", rec)
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, 400, "invalid json: "+err.Error())
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeError(w, 400, "invalid json: body must contain exactly one JSON object")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx > -1 {
		return r.RemoteAddr[:idx]
	}
	return r.RemoteAddr
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func nullIfEmpty(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}
func valueOr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
func validLatLng(lat, lng float64) bool {
	return lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}

func validOptionalLatLng(lat, lng *float64) bool {
	if lat == nil && lng == nil {
		return true
	}
	if lat == nil || lng == nil {
		return false
	}
	return validLatLng(*lat, *lng)
}

func limitOffset(r *http.Request, defaultLimit, maxLimit int) (int, int) {
	limit := atoiDefault(r.URL.Query().Get("limit"), defaultLimit)
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	offset := atoiDefault(r.URL.Query().Get("offset"), 0)
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func validRole(role string) bool {
	switch role {
	case "owner", "admin", "manager", "sales", "employee":
		return true
	default:
		return false
	}
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func writeCSV(w http.ResponseWriter, filename string, records [][]string) {
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	_ = cw.WriteAll(records)
	cw.Flush()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func ptrString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func i64(v int64) string {
	return strconv.FormatInt(v, 10)
}

func queryDate(r *http.Request, key string, def time.Time) time.Time {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	if v == "" {
		return def
	}
	if t, err := time.Parse("2006-01-02", v); err == nil {
		return t
	}
	return def
}
func queryRange(r *http.Request) (time.Time, time.Time) {
	now := time.Now()
	from := queryDate(r, "from", time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC))
	toRaw := strings.TrimSpace(r.URL.Query().Get("to"))
	if toRaw == "" {
		return from, from.AddDate(0, 1, 0)
	}
	to := queryDate(r, "to", from.AddDate(0, 1, 0))
	return from, to.Add(24 * time.Hour)
}
func parseMonth(v string) (time.Time, error) {
	if len(v) == 7 {
		return time.Parse("2006-01", v)
	}
	if len(v) >= 10 {
		t, err := time.Parse("2006-01-02", v[:10])
		if err != nil {
			return time.Time{}, err
		}
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC), nil
	}
	return time.Time{}, fmt.Errorf("invalid month")
}
func periodBounds(period string, base time.Time) (time.Time, time.Time) {
	b := time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, time.UTC)
	switch period {
	case "weekly":
		wd := int(b.Weekday())
		if wd == 0 {
			wd = 7
		}
		start := b.AddDate(0, 0, -wd+1)
		return start, start.AddDate(0, 0, 7)
	case "monthly":
		start := time.Date(b.Year(), b.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0)
	case "yearly":
		start := time.Date(b.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(1, 0, 0)
	default:
		return b, b.Add(24 * time.Hour)
	}
}
func atoiDefault(v string, def int) int {
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return def
}
