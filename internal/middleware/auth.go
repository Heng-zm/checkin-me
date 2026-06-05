package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/hengk7401/checkinme-go-api/internal/security"
)

type ctxKey string

const ClaimsKey ctxKey = "claims"

func Auth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				writeAuthError(w, "missing bearer token")
				return
			}
			claims, err := security.ParseToken(jwtSecret, strings.TrimPrefix(header, "Bearer "))
			if err != nil {
				writeAuthError(w, "invalid token")
				return
			}
			ctx := context.WithValue(r.Context(), ClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func Claims(r *http.Request) *security.UserClaims {
	claims, _ := r.Context().Value(ClaimsKey).(*security.UserClaims)
	return claims
}

func RequireRoles(roles ...string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, role := range roles {
		allowed[role] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := Claims(r)
			if claims == nil || !allowed[claims.Role] {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "forbidden"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": msg})
}
