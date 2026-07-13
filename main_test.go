package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	newServer(Config{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	want := `{"status":"ok"}`
	if got := rec.Body.String(); got != want {
		t.Fatalf("expected body %q, got %q", want, got)
	}
}

func TestServerAppliesCORSToAuthRoutes(t *testing.T) {
	pool := setupTestDB(t)
	req := httptest.NewRequest(http.MethodOptions, "/auth/login", nil)
	req.Header.Set("Origin", "https://portfolio-site-gold-alpha.vercel.app")
	rec := httptest.NewRecorder()

	newServer(Config{Pool: pool}).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://portfolio-site-gold-alpha.vercel.app" {
		t.Fatalf("expected CORS header to echo allowed origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, Authorization" {
		t.Fatalf("expected Authorization allowed in CORS headers, got %q", got)
	}
}

func TestServerOmitsCORSForDisallowedOrigin(t *testing.T) {
	pool := setupTestDB(t)
	req := httptest.NewRequest(http.MethodOptions, "/auth/login", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()

	newServer(Config{Pool: pool}).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS header for disallowed origin, got %q", got)
	}
}

func TestServerWebhookRouteHasNoCORS(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/billing/webhook", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://portfolio-site-gold-alpha.vercel.app")
	rec := httptest.NewRecorder()

	newServer(Config{StripeWebhookSecret: "whsec_test"}).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS header on webhook route, got %q", got)
	}
}
