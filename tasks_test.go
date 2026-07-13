package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func createTask(t *testing.T, pool *pgxpool.Pool, token, projectID, title string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"title": title})
	req := httptest.NewRequest(http.MethodPost, "/projects/"+projectID+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", projectID)
	rec := httptest.NewRecorder()
	requireAuth(pool, createTaskHandler(pool)).ServeHTTP(rec, req)
	var task Task
	json.Unmarshal(rec.Body.Bytes(), &task)
	return task.ID
}

func TestCreateAndListTasks(t *testing.T) {
	pool := setupTestDB(t)
	token := signupAndLogin(t, pool, "owner@acme.test")
	projectID := createProject(t, pool, token, "Website Redesign")
	createTask(t, pool, token, projectID, "Write homepage copy")

	req := httptest.NewRequest(http.MethodGet, "/projects/"+projectID+"/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", projectID)
	rec := httptest.NewRecorder()
	requireAuth(pool, listTasksHandler(pool)).ServeHTTP(rec, req)

	var tasks []Task
	json.Unmarshal(rec.Body.Bytes(), &tasks)
	if len(tasks) != 1 || tasks[0].Title != "Write homepage copy" || tasks[0].Status != "todo" {
		t.Fatalf("expected one todo task, got %+v", tasks)
	}
}

func TestListTasksReturns404ForOtherOrgProject(t *testing.T) {
	pool := setupTestDB(t)
	tokenA := signupAndLogin(t, pool, "owner@acme.test")
	tokenB := signupAndLogin(t, pool, "owner@other.test")
	projectID := createProject(t, pool, tokenA, "Acme Internal")
	createTask(t, pool, tokenA, projectID, "Confidential task")

	req := httptest.NewRequest(http.MethodGet, "/projects/"+projectID+"/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+tokenB)
	req.SetPathValue("id", projectID)
	rec := httptest.NewRecorder()
	requireAuth(pool, listTasksHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 listing tasks under another org's project, got %d", rec.Code)
	}
}

func TestCreateTaskReturns404ForOtherOrgProject(t *testing.T) {
	pool := setupTestDB(t)
	tokenA := signupAndLogin(t, pool, "owner@acme.test")
	tokenB := signupAndLogin(t, pool, "owner@other.test")
	projectID := createProject(t, pool, tokenA, "Acme Internal")

	body, _ := json.Marshal(map[string]string{"title": "Sneaky task"})
	req := httptest.NewRequest(http.MethodPost, "/projects/"+projectID+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tokenB)
	req.SetPathValue("id", projectID)
	rec := httptest.NewRecorder()
	requireAuth(pool, createTaskHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 creating a task under another org's project, got %d", rec.Code)
	}
}

func TestUpdateTaskChangesStatus(t *testing.T) {
	pool := setupTestDB(t)
	token := signupAndLogin(t, pool, "owner@acme.test")
	projectID := createProject(t, pool, token, "Website Redesign")
	taskID := createTask(t, pool, token, projectID, "Write homepage copy")

	body, _ := json.Marshal(map[string]string{"status": "done"})
	req := httptest.NewRequest(http.MethodPatch, "/tasks/"+taskID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	requireAuth(pool, updateTaskHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateTaskRejectsInvalidStatus(t *testing.T) {
	pool := setupTestDB(t)
	token := signupAndLogin(t, pool, "owner@acme.test")
	projectID := createProject(t, pool, token, "Website Redesign")
	taskID := createTask(t, pool, token, projectID, "Write homepage copy")

	body, _ := json.Marshal(map[string]string{"status": "not-a-real-status"})
	req := httptest.NewRequest(http.MethodPatch, "/tasks/"+taskID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	requireAuth(pool, updateTaskHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestUpdateTaskReturns404ForOtherOrg(t *testing.T) {
	pool := setupTestDB(t)
	tokenA := signupAndLogin(t, pool, "owner@acme.test")
	tokenB := signupAndLogin(t, pool, "owner@other.test")
	projectID := createProject(t, pool, tokenA, "Acme Internal")
	taskID := createTask(t, pool, tokenA, projectID, "Confidential task")

	body, _ := json.Marshal(map[string]string{"status": "done"})
	req := httptest.NewRequest(http.MethodPatch, "/tasks/"+taskID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tokenB)
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	requireAuth(pool, updateTaskHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 updating another org's task, got %d", rec.Code)
	}
}

func TestDeleteTaskReturns404ForOtherOrg(t *testing.T) {
	pool := setupTestDB(t)
	tokenA := signupAndLogin(t, pool, "owner@acme.test")
	tokenB := signupAndLogin(t, pool, "owner@other.test")
	projectID := createProject(t, pool, tokenA, "Acme Internal")
	taskID := createTask(t, pool, tokenA, projectID, "Confidential task")

	req := httptest.NewRequest(http.MethodDelete, "/tasks/"+taskID, nil)
	req.Header.Set("Authorization", "Bearer "+tokenB)
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	requireAuth(pool, deleteTaskHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting another org's task, got %d", rec.Code)
	}
}

func TestDeleteTaskSucceedsForOwningOrg(t *testing.T) {
	pool := setupTestDB(t)
	token := signupAndLogin(t, pool, "owner@acme.test")
	projectID := createProject(t, pool, token, "Website Redesign")
	taskID := createTask(t, pool, token, projectID, "Write homepage copy")

	req := httptest.NewRequest(http.MethodDelete, "/tasks/"+taskID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	requireAuth(pool, deleteTaskHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}
