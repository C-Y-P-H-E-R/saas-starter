package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const sessionTTL = 7 * 24 * time.Hour
const minPasswordLength = 8

type Session struct {
	UserID string
	OrgID  string
	Role   string
}

type contextKey string

const sessionContextKey contextKey = "session"

func sessionFromContext(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(sessionContextKey).(Session)
	return s, ok
}

func requireAuth(pool *pgxpool.Pool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var s Session
		err := pool.QueryRow(r.Context(), `
			SELECT s.user_id, s.org_id, m.role
			FROM sessions s
			JOIN memberships m ON m.user_id = s.user_id AND m.org_id = s.org_id
			WHERE s.token = $1 AND s.expires_at > now()`,
			token,
		).Scan(&s.UserID, &s.OrgID, &s.Role)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), sessionContextKey, s)
		next(w, r.WithContext(ctx))
	}
}

func signupHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			OrgName  string `json:"org_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if !strings.Contains(payload.Email, "@") || payload.OrgName == "" {
			http.Error(w, "email and org_name are required", http.StatusBadRequest)
			return
		}
		if len(payload.Password) < minPasswordLength {
			http.Error(w, "password too short", http.StatusBadRequest)
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(payload.Password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		ctx := r.Context()
		tx, err := pool.Begin(ctx)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(ctx)

		userID := newID()
		if _, err := tx.Exec(ctx, "INSERT INTO users (id, email, password_hash) VALUES ($1, $2, $3)", userID, payload.Email, string(hash)); err != nil {
			if isUniqueViolation(err) {
				http.Error(w, "email already registered", http.StatusConflict)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		orgID := newID()
		if _, err := tx.Exec(ctx, "INSERT INTO orgs (id, name) VALUES ($1, $2)", orgID, payload.OrgName); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		if _, err := tx.Exec(ctx, "INSERT INTO memberships (user_id, org_id, role) VALUES ($1, $2, 'owner')", userID, orgID); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		token := createSessionToken()
		if _, err := tx.Exec(ctx, "INSERT INTO sessions (token, user_id, org_id, expires_at) VALUES ($1, $2, $3, $4)", token, userID, orgID, time.Now().Add(sessionTTL)); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		if err := tx.Commit(ctx); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"token": token})
	}
}

func loginHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		var userID, hash string
		err := pool.QueryRow(ctx, "SELECT id, password_hash FROM users WHERE email = $1", payload.Email).Scan(&userID, &hash)
		if err != nil {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(payload.Password)); err != nil {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		var orgID string
		err = pool.QueryRow(ctx, "SELECT org_id FROM memberships WHERE user_id = $1 ORDER BY created_at ASC LIMIT 1", userID).Scan(&orgID)
		if err != nil {
			http.Error(w, "no organization found for this user", http.StatusUnauthorized)
			return
		}

		token := createSessionToken()
		if _, err := pool.Exec(ctx, "INSERT INTO sessions (token, user_id, org_id, expires_at) VALUES ($1, $2, $3, $4)", token, userID, orgID, time.Now().Add(sessionTTL)); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": token})
	}
}

func logoutHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != "" {
			pool.Exec(r.Context(), "DELETE FROM sessions WHERE token = $1", token)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func meHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, ok := sessionFromContext(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var email, orgName string
		err := pool.QueryRow(r.Context(),
			"SELECT u.email, o.name FROM users u JOIN orgs o ON o.id = $2 WHERE u.id = $1",
			s.UserID, s.OrgID,
		).Scan(&email, &orgName)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"userId":  s.UserID,
			"email":   email,
			"orgId":   s.OrgID,
			"orgName": orgName,
			"role":    s.Role,
		})
	}
}

func createSessionToken() string {
	return newID() + newID()
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
