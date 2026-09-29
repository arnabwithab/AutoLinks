// ----- Clerk JWT auth middleware @ backend/internal/auth/middleware.go -----
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arnabwithab/AutoLinks/backend/internal/logger"
	"github.com/clerkinc/clerk-sdk-go/clerk"
)

type contextKey string

const userIDKey contextKey = "clerk_user_id"

// TokenVerifier is the subset of clerk.Client that the middleware needs.
type TokenVerifier interface {
	VerifyToken(token string, opts ...clerk.VerifyTokenOption) (*clerk.SessionClaims, error)
}

// UserIDFromContext extracts the Clerk user ID from the request context.
func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(userIDKey).(string); ok {
		return v
	}
	return ""
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"detail": "unauthorized"})
}

// RequireAuth returns a chi middleware that verifies Clerk session JWTs.
func RequireAuth(client TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" || !strings.HasPrefix(header, "Bearer ") {
				writeUnauthorized(w)
				return
			}

			token := strings.TrimPrefix(header, "Bearer ")
			claims, err := client.VerifyToken(token)
			if err != nil {
				logger.Error("Token verification failed: %s", err)
				writeUnauthorized(w)
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey, claims.Subject)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
