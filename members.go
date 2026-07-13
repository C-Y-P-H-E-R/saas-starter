package main

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

func isAdminOrOwner(role string) bool {
	return role == "owner" || role == "admin"
}

func isValidRole(role string) bool {
	return role == "owner" || role == "admin" || role == "member"
}

func listMembersHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessionFromContext(r.Context())

		rows, err := pool.Query(r.Context(),
			"SELECT u.id, u.email, m.role FROM memberships m JOIN users u ON u.id = m.user_id WHERE m.org_id = $1 ORDER BY m.created_at ASC",
			s.OrgID,
		)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type member struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Role  string `json:"role"`
		}
		members := []member{}
		for rows.Next() {
			var m member
			if err := rows.Scan(&m.ID, &m.Email, &m.Role); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			members = append(members, m)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(members)
	}
}

func addMemberHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessionFromContext(r.Context())
		if !isAdminOrOwner(s.Role) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		var payload struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || !isValidRole(payload.Role) {
			http.Error(w, "email and a valid role are required", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		var targetUserID string
		if err := pool.QueryRow(ctx, "SELECT id FROM users WHERE email = $1", payload.Email).Scan(&targetUserID); err != nil {
			http.Error(w, "user must sign up first", http.StatusNotFound)
			return
		}

		_, err := pool.Exec(ctx, "INSERT INTO memberships (user_id, org_id, role) VALUES ($1, $2, $3)", targetUserID, s.OrgID, payload.Role)
		if err != nil {
			if isUniqueViolation(err) {
				http.Error(w, "user is already a member", http.StatusConflict)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}

func updateMemberRoleHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessionFromContext(r.Context())
		if !isAdminOrOwner(s.Role) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		targetUserID := r.PathValue("userId")
		var payload struct {
			Role string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || !isValidRole(payload.Role) {
			http.Error(w, "a valid role is required", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		var currentRole string
		if err := pool.QueryRow(ctx, "SELECT role FROM memberships WHERE user_id = $1 AND org_id = $2", targetUserID, s.OrgID).Scan(&currentRole); err != nil {
			http.Error(w, "member not found", http.StatusNotFound)
			return
		}

		if currentRole == "owner" && payload.Role != "owner" {
			if lastOwner(ctx, pool, s.OrgID) {
				http.Error(w, "cannot demote the last owner", http.StatusBadRequest)
				return
			}
		}

		if _, err := pool.Exec(ctx, "UPDATE memberships SET role = $1 WHERE user_id = $2 AND org_id = $3", payload.Role, targetUserID, s.OrgID); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

func removeMemberHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessionFromContext(r.Context())
		if !isAdminOrOwner(s.Role) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		targetUserID := r.PathValue("userId")
		ctx := r.Context()

		var currentRole string
		if err := pool.QueryRow(ctx, "SELECT role FROM memberships WHERE user_id = $1 AND org_id = $2", targetUserID, s.OrgID).Scan(&currentRole); err != nil {
			http.Error(w, "member not found", http.StatusNotFound)
			return
		}

		if currentRole == "owner" && lastOwner(ctx, pool, s.OrgID) {
			http.Error(w, "cannot remove the last owner", http.StatusBadRequest)
			return
		}

		if _, err := pool.Exec(ctx, "DELETE FROM memberships WHERE user_id = $1 AND org_id = $2", targetUserID, s.OrgID); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func lastOwner(ctx context.Context, pool *pgxpool.Pool, orgID string) bool {
	var count int
	pool.QueryRow(ctx, "SELECT COUNT(*) FROM memberships WHERE org_id = $1 AND role = 'owner'", orgID).Scan(&count)
	return count <= 1
}
