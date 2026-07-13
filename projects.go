package main

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func listProjectsHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessionFromContext(r.Context())
		rows, err := pool.Query(r.Context(), "SELECT id, name FROM projects WHERE org_id = $1 ORDER BY created_at ASC", s.OrgID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		projects := []Project{}
		for rows.Next() {
			var p Project
			if err := rows.Scan(&p.ID, &p.Name); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			projects = append(projects, p)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(projects)
	}
}

func createProjectHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessionFromContext(r.Context())
		var payload struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		id := newID()
		if _, err := pool.Exec(r.Context(), "INSERT INTO projects (id, org_id, name) VALUES ($1, $2, $3)", id, s.OrgID, payload.Name); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(Project{ID: id, Name: payload.Name})
	}
}

func getProjectHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessionFromContext(r.Context())
		id := r.PathValue("id")

		var p Project
		err := pool.QueryRow(r.Context(), "SELECT id, name FROM projects WHERE id = $1 AND org_id = $2", id, s.OrgID).Scan(&p.ID, &p.Name)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(p)
	}
}

func updateProjectHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessionFromContext(r.Context())
		id := r.PathValue("id")

		var payload struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		tag, err := pool.Exec(r.Context(), "UPDATE projects SET name = $1 WHERE id = $2 AND org_id = $3", payload.Name, id, s.OrgID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

func deleteProjectHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessionFromContext(r.Context())
		id := r.PathValue("id")

		tag, err := pool.Exec(r.Context(), "DELETE FROM projects WHERE id = $1 AND org_id = $2", id, s.OrgID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if tag.RowsAffected() == 0 {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
