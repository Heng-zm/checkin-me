package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv                string
	Port                  string
	DatabaseURL           string
	JWTSecret             string
	CorsAllowedOrigins    []string
	TelegramBotToken      string
	TelegramDefaultChatID string
	DeviceWebhookSecret   string
	DefaultTimezone       string
	BankProvider          string
	BankAPIBaseURL        string
	BankAPIKey            string
	DBMaxConns            int32
	DBMinConns            int32
	DBMaxConnIdleMinutes  int
	DBQueryExecMode       string
	DBSchema              string
	RequestTimeoutSeconds int
	SlowRequestMS         int
	CacheTTLSeconds       int
	AsyncWorkerLimit      int
	FraudWarnScore        int
	FraudBlockScore       int
	FraudMaxSpeedKPH      float64
	FraudMaxGPSAccuracyM  int
	FraudDuplicateSeconds int
	AutoMigrate           bool
}

func Load() Config {
	_ = godotenv.Load()
	return Config{
		AppEnv:                env("APP_ENV", "local"),
		Port:                  env("PORT", "8080"),
		DatabaseURL:           env("DATABASE_URL", "postgres://checkinme:checkinme@localhost:5432/checkinme?sslmode=disable"),
		JWTSecret:             env("JWT_SECRET", "dev-secret-change-me"),
		CorsAllowedOrigins:    splitCSV(env("CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:5173")),
		TelegramBotToken:      env("TELEGRAM_BOT_TOKEN", ""),
		TelegramDefaultChatID: env("TELEGRAM_DEFAULT_CHAT_ID", ""),
		DeviceWebhookSecret:   env("DEVICE_WEBHOOK_SECRET", "dev-device-secret-change-me"),
		DefaultTimezone:       env("DEFAULT_TIMEZONE", "Asia/Phnom_Penh"),
		BankProvider:          env("BANK_PROVIDER", "manual_csv"),
		BankAPIBaseURL:        env("BANK_API_BASE_URL", ""),
		BankAPIKey:            env("BANK_API_KEY", ""),
		DBMaxConns:            int32(envInt("DB_MAX_CONNS", 20)),
		DBMinConns:            int32(envInt("DB_MIN_CONNS", 2)),
		DBMaxConnIdleMinutes:  envInt("DB_MAX_CONN_IDLE_MINUTES", 10),
		DBQueryExecMode:       env("DB_QUERY_EXEC_MODE", "auto"),
		DBSchema:              env("DB_SCHEMA", "checkinme"),
		RequestTimeoutSeconds: envInt("REQUEST_TIMEOUT_SECONDS", 15),
		SlowRequestMS:         envInt("SLOW_REQUEST_MS", 700),
		CacheTTLSeconds:       envInt("CACHE_TTL_SECONDS", 60),
		AsyncWorkerLimit:      envInt("ASYNC_WORKER_LIMIT", 8),
		FraudWarnScore:        envInt("FRAUD_WARN_SCORE", 40),
		FraudBlockScore:       envInt("FRAUD_BLOCK_SCORE", 100),
		FraudMaxSpeedKPH:      envFloat("FRAUD_MAX_SPEED_KPH", 180),
		FraudMaxGPSAccuracyM:  envInt("FRAUD_MAX_GPS_ACCURACY_M", 80),
		FraudDuplicateSeconds: envInt("FRAUD_DUPLICATE_SECONDS", 120),
		AutoMigrate:           envBool("AUTO_MIGRATE", true),
	}
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (c Config) Validate() error {
	if c.AppEnv != "local" {
		if len(c.JWTSecret) < 32 || c.JWTSecret == "dev-secret-change-me" || c.JWTSecret == "change-this-long-random-secret" {
			return fmt.Errorf("JWT_SECRET must be at least 32 characters in non-local environments")
		}
		if len(c.DeviceWebhookSecret) < 24 || c.DeviceWebhookSecret == "dev-device-secret-change-me" || c.DeviceWebhookSecret == "change-this-device-webhook-secret" {
			return fmt.Errorf("DEVICE_WEBHOOK_SECRET must be at least 24 characters in non-local environments")
		}
	}
	if c.DBMaxConns < 1 {
		return fmt.Errorf("DB_MAX_CONNS must be greater than 0")
	}
	if c.DBMinConns < 0 || c.DBMinConns > c.DBMaxConns {
		return fmt.Errorf("DB_MIN_CONNS must be between 0 and DB_MAX_CONNS")
	}
	if c.DBQueryExecMode != "auto" && c.DBQueryExecMode != "simple_protocol" {
		return fmt.Errorf("DB_QUERY_EXEC_MODE must be auto or simple_protocol")
	}
	if !validDBSchema(c.DBSchema) {
		return fmt.Errorf("DB_SCHEMA must be a safe PostgreSQL identifier")
	}
	if c.RequestTimeoutSeconds < 1 {
		return fmt.Errorf("REQUEST_TIMEOUT_SECONDS must be greater than 0")
	}
	if c.AsyncWorkerLimit < 1 {
		return fmt.Errorf("ASYNC_WORKER_LIMIT must be greater than 0")
	}
	if c.CacheTTLSeconds < 0 {
		return fmt.Errorf("CACHE_TTL_SECONDS cannot be negative")
	}
	if c.FraudWarnScore < 1 || c.FraudBlockScore < c.FraudWarnScore {
		return fmt.Errorf("fraud scores must satisfy 1 <= FRAUD_WARN_SCORE <= FRAUD_BLOCK_SCORE")
	}
	if c.FraudMaxSpeedKPH <= 0 {
		return fmt.Errorf("FRAUD_MAX_SPEED_KPH must be greater than 0")
	}
	return nil
}

func validDBSchema(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	for i, r := range v {
		if i == 0 {
			if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
				return false
			}
			continue
		}
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envBool(key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	switch v {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func envFloat(key string, fallback float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return n
}
