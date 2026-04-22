package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// passthrough tracks whether the downstream handler was invoked, so the
// test can assert that the auth gate rejected before dispatch.
type passthrough struct{ called bool }

func (p *passthrough) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.called = true
	w.WriteHeader(http.StatusOK)
}

func TestBearerMiddleware_RejectsMissingAuthorization(t *testing.T) {
	next := &passthrough{}
	h := withBearerAuth("secret-token", next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d want 401", rec.Code)
	}
	if next.called {
		t.Error("downstream must not run when auth missing")
	}
}

func TestBearerMiddleware_RejectsWrongToken(t *testing.T) {
	next := &passthrough{}
	h := withBearerAuth("secret-token", next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d want 401", rec.Code)
	}
	if next.called {
		t.Error("downstream must not run when token wrong")
	}
}

func TestBearerMiddleware_RejectsMalformedScheme(t *testing.T) {
	next := &passthrough{}
	h := withBearerAuth("secret-token", next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	// No "Bearer " prefix.
	req.Header.Set("Authorization", "secret-token")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d want 401", rec.Code)
	}
	if next.called {
		t.Error("downstream must not run with non-Bearer scheme")
	}
}

func TestBearerMiddleware_PassesValidToken(t *testing.T) {
	next := &passthrough{}
	h := withBearerAuth("secret-token", next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d want 200", rec.Code)
	}
	if !next.called {
		t.Error("downstream must run for valid token")
	}
}

func TestResolveToken_RejectsMissingEnvVar(t *testing.T) {
	// --http with --token-env unset is a refuse-to-start condition: an open
	// HTTP endpoint on a shared $MGTT_HOME is never intentional.
	_, err := resolveToken(Config{HTTP: true, TokenEnv: ""})
	if err == nil {
		t.Fatal("expected error when HTTP mode and token_env unset")
	}
}

func TestResolveToken_RejectsEmptyNamedEnvVar(t *testing.T) {
	t.Setenv("MGTT_MCP_TOKEN_EMPTY", "") // explicitly empty
	if _, err := resolveToken(Config{HTTP: true, TokenEnv: "MGTT_MCP_TOKEN_EMPTY"}); err == nil {
		t.Fatal("expected error when named env var is empty")
	}
}

func TestResolveToken_ReturnsTokenWhenSet(t *testing.T) {
	t.Setenv("MGTT_MCP_TOKEN_OK", "sekret")
	tok, err := resolveToken(Config{HTTP: true, TokenEnv: "MGTT_MCP_TOKEN_OK"})
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if tok != "sekret" {
		t.Errorf("token: got %q want %q", tok, "sekret")
	}
}
