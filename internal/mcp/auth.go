// Copyright (C) 2026 Alex Kunich
// SPDX-License-Identifier: AGPL-3.0-or-later

package mcp

import (
	"crypto/sha256"
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
// scheme is matched case-insensitively per RFC 7235 (clients that send
// "bearer" or "BEARER" are accepted); the token itself is compared by
// SHA-256 digest so length-mismatch cannot leak via subtle's early
// return. Any failure returns 401 before the request reaches the MCP
// dispatcher.
func withBearerAuth(expected string, next http.Handler) http.Handler {
	expectedDigest := sha256.Sum256([]byte(expected))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := extractBearer(r.Header.Get("Authorization"))
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		gotDigest := sha256.Sum256([]byte(token))
		if subtle.ConstantTimeCompare(gotDigest[:], expectedDigest[:]) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// extractBearer returns the token from an Authorization header value.
// Matches "Bearer" / "bearer" / "BEARER" followed by whitespace and a
// token. Returns ok=false for any other shape.
func extractBearer(header string) (string, bool) {
	const scheme = "bearer"
	if len(header) <= len(scheme) {
		return "", false
	}
	if !strings.EqualFold(header[:len(scheme)], scheme) {
		return "", false
	}
	sep := header[len(scheme)]
	if sep != ' ' && sep != '\t' {
		return "", false
	}
	token := strings.TrimLeft(header[len(scheme):], " \t")
	if token == "" {
		return "", false
	}
	return token, true
}
