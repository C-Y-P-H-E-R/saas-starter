package main

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Task struct {
	ID             string  `json:"id"`
	ProjectID      string  `json:"projectId"`
	Title          string  `json:"title"`
	Status         string  `json:"status"`
	AssigneeUserID *string `json:"assigneeUserId,omitempty"`
}

func isValidTaskStatus(status string) bool {
	return status == "todo" || status == "in_progress" || status == "done"
}

func listTasksHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessionFromContext(r.Context())
		projectID := r.PathValue("id")
		ctx := r.Context()

		var exists bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM projects WHERE id = $1 AND org_id = $2)", projectID, s.OrgID).Scan(&exists); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !exists {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		rows, err := pool.Query(ctx, "SELECT id, project_id, title, status, assignee_user_id FROM tasks WHERE project_id = $1 AND org_id = $2 ORDER BY created_at ASC", projectID, s.OrgID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		tasks := []Task{}
		for rows.Next() {
			var t Task
			if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.AssigneeUserID); err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			tasks = append(tasks, t)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tasks)
	}
}

func createTaskHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessionFromContext(r.Context())
		projectID := r.PathValue("id")
		ctx := r.Context()

		var payload struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Title == "" {
			http.Error(w, "title is required", http.StatusBadRequest)
			return
		}

		var exists bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM projects WHERE id = $1 AND org_id = $2)", projectID, s.OrgID).Scan(&exists); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !exists {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		id := newID()
		if _, err := pool.Exec(ctx, "INSERT INTO tasks (id, project_id, org_id, title, status) VALUES ($1, $2, $3, $4, 'todo')", id, projectID, s.OrgID, payload.Title); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(Task{ID: id, ProjectID: projectID, Title: payload.Title, Status: "todo"})
	}
}

func updateTaskHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessionFromContext(r.Context())
		id := r.PathValue("id")
		ctx := r.Context()

		var payload struct {
			Title          *string `json:"title"`
			Status         *string `json:"status"`
			AssigneeUserID *string `json:"assigneeUserId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if payload.Status != nil && !isValidTaskStatus(*payload.Status) {
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}

		var current Task
		err := pool.QueryRow(ctx, "SELECT id, project_id, title, status, assignee_user_id FROM tasks WHERE id = $1 AND org_id = $2", id, s.OrgID).
			Scan(&current.ID, &current.ProjectID, &current.Title, &current.Status, &current.AssigneeUserID)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		title := current.Title
		if payload.Title != nil {
			title = *payload.Title
		}
		status := current.Status
		if payload.Status != nil {
			status = *payload.Status
		}
		assignee := current.AssigneeUserID
		if payload.AssigneeUserID != nil {
			assignee = payload.AssigneeUserID
		}

		if _, err := pool.Exec(ctx, "UPDATE tasks SET title = $1, status = $2, assignee_user_id = $3 WHERE id = $4 AND org_id = $5", title, status, assignee, id, s.OrgID); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

func deleteTaskHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessionFromContext(r.Context())
		id := r.PathValue("id")

		tag, err := pool.Exec(r.Context(), "DELETE FROM tasks WHERE id = $1 AND org_id = $2", id, s.OrgID)
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
