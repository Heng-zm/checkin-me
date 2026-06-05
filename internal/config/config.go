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
	return nil
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
