// server/api/auth.go
package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// RequireToken guards routes with `Authorization: Bearer <token>`. An
// empty configured token fails closed with 503 so a misconfigured deploy
// never runs the API open.
func RequireToken(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				writeError(w, http.StatusServiceUnavailable, "api_disabled",
					"API disabled: set --api-token or OPENBOOKS_API_TOKEN")
				return
			}
			const prefix = "Bearer "
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, prefix) ||
				subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, prefix)), []byte(token)) != 1 {
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
