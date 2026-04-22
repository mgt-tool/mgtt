package mcp

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// resolveToken returns the bearer token mgtt will compare against incoming
// Authorization headers. HTTP mode without a configured, non-empty token
// refuses to start: a publicly-reachable MCP endpoint bound to a shared
// $MGTT_HOME is never intentional.
func resolveToken(cfg Config) (string, error) {
	if !cfg.HTTP {
		return "", nil
	}
	if cfg.TokenEnv == "" {
		return "", fmt.Errorf("--http requires --token-env naming the env var that holds the bearer token")
	}
	v := os.Getenv(cfg.TokenEnv)
	if v == "" {
		return "", fmt.Errorf("--token-env %q is empty; refusing to serve an unauthenticated HTTP endpoint", cfg.TokenEnv)
	}
	return v, nil
}

// withBearerAuth wraps next with a constant-time bearer-token check. The
// header must be exactly `Authorization: Bearer <token>`. Any failure
// returns 401 before the request reaches the MCP dispatcher.
func withBearerAuth(expected string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(got, prefix) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		token := got[len(prefix):]
		if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
