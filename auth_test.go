package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSignupCreatesOrgUserAndSession(t *testing.T) {
	pool := setupTestDB(t)
	body, _ := json.Marshal(map[string]string{
		"email":    "owner@acme.test",
		"password": "correct-horse",
		"org_name": "Acme",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/signup", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	signupHandler(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected a non-empty token")
	}
}

func TestSignupRejectsShortPassword(t *testing.T) {
	pool := setupTestDB(t)
	body, _ := json.Marshal(map[string]string{
		"email":    "owner@acme.test",
		"password": "short",
		"org_name": "Acme",
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/signup", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	signupHandler(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSignupRejectsDuplicateEmail(t *testing.T) {
	pool := setupTestDB(t)
	body, _ := json.Marshal(map[string]string{
		"email":    "owner@acme.test",
		"password": "correct-horse",
		"org_name": "Acme",
	})

	req1 := httptest.NewRequest(http.MethodPost, "/auth/signup", bytes.NewReader(body))
	signupHandler(pool).ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodPost, "/auth/signup", bytes.NewReader(body))
	rec2 := httptest.NewRecorder()
	signupHandler(pool).ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409 on duplicate email, got %d", rec2.Code)
	}
}

func TestLoginReturnsSessionTokenOnCorrectPassword(t *testing.T) {
	pool := setupTestDB(t)
	signupBody, _ := json.Marshal(map[string]string{
		"email": "owner@acme.test", "password": "correct-horse", "org_name": "Acme",
	})
	signupHandler(pool).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/auth/signup", bytes.NewReader(signupBody)))

	loginBody, _ := json.Marshal(map[string]string{"email": "owner@acme.test", "password": "correct-horse"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(loginBody))
	rec := httptest.NewRecorder()

	loginHandler(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	pool := setupTestDB(t)
	signupBody, _ := json.Marshal(map[string]string{
		"email": "owner@acme.test", "password": "correct-horse", "org_name": "Acme",
	})
	signupHandler(pool).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/auth/signup", bytes.NewReader(signupBody)))

	loginBody, _ := json.Marshal(map[string]string{"email": "owner@acme.test", "password": "wrong-password"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(loginBody))
	rec := httptest.NewRecorder()

	loginHandler(pool).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func signupAndLogin(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	signupBody, _ := json.Marshal(map[string]string{
		"email": email, "password": "correct-horse", "org_name": "Acme",
	})
	rec := httptest.NewRecorder()
	signupHandler(pool).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/auth/signup", bytes.NewReader(signupBody)))
	var resp struct {
		Token string `json:"token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Token
}

func TestRequireAuthRejectsMissingToken(t *testing.T) {
	pool := setupTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	rec := httptest.NewRecorder()

	requireAuth(pool, meHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireAuthAcceptsValidTokenAndInjectsSession(t *testing.T) {
	pool := setupTestDB(t)
	token := signupAndLogin(t, pool, "owner@acme.test")

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	meHandlerWithAuth := requireAuth(pool, meHandler(pool))
	meHandlerWithAuth.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Role string `json:"role"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Role != "owner" {
		t.Fatalf("expected role %q, got %q", "owner", resp.Role)
	}
}

func TestLogoutDeletesSession(t *testing.T) {
	pool := setupTestDB(t)
	token := signupAndLogin(t, pool, "owner@acme.test")

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	logoutHandler(pool).ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest(http.MethodGet, "/me", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	requireAuth(pool, meHandler(pool)).ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", rec2.Code)
	}
}
