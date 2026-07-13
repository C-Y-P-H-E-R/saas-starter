package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func createProject(t *testing.T, pool *pgxpool.Pool, token, name string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	req := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	requireAuth(pool, createProjectHandler(pool)).ServeHTTP(rec, req)
	var p Project
	json.Unmarshal(rec.Body.Bytes(), &p)
	return p.ID
}

func TestCreateAndListProjects(t *testing.T) {
	pool := setupTestDB(t)
	token := signupAndLogin(t, pool, "owner@acme.test")
	createProject(t, pool, token, "Website Redesign")

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	requireAuth(pool, listProjectsHandler(pool)).ServeHTTP(rec, req)

	var projects []Project
	json.Unmarshal(rec.Body.Bytes(), &projects)
	if len(projects) != 1 || projects[0].Name != "Website Redesign" {
		t.Fatalf("expected one project named %q, got %+v", "Website Redesign", projects)
	}
}

func TestListProjectsOnlyReturnsCallersOrg(t *testing.T) {
	pool := setupTestDB(t)
	tokenA := signupAndLogin(t, pool, "owner@acme.test")
	tokenB := signupAndLogin(t, pool, "owner@other.test")
	createProject(t, pool, tokenA, "Acme Internal")
	createProject(t, pool, tokenB, "Other Co Internal")

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req.Header.Set("Authorization", "Bearer "+tokenA)
	rec := httptest.NewRecorder()
	requireAuth(pool, listProjectsHandler(pool)).ServeHTTP(rec, req)

	var projects []Project
	json.Unmarshal(rec.Body.Bytes(), &projects)
	if len(projects) != 1 || projects[0].Name != "Acme Internal" {
		t.Fatalf("expected only the caller's own org's project, got %+v", projects)
	}
}

func TestGetProjectReturns404ForOtherOrg(t *testing.T) {
	pool := setupTestDB(t)
	tokenA := signupAndLogin(t, pool, "owner@acme.test")
	tokenB := signupAndLogin(t, pool, "owner@other.test")
	projectID := createProject(t, pool, tokenA, "Acme Internal")

	req := httptest.NewRequest(http.MethodGet, "/projects/"+projectID, nil)
	req.Header.Set("Authorization", "Bearer "+tokenB)
	req.SetPathValue("id", projectID)
	rec := httptest.NewRecorder()
	requireAuth(pool, getProjectHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 reading another org's project, got %d", rec.Code)
	}
}

func TestUpdateProjectReturns404ForOtherOrg(t *testing.T) {
	pool := setupTestDB(t)
	tokenA := signupAndLogin(t, pool, "owner@acme.test")
	tokenB := signupAndLogin(t, pool, "owner@other.test")
	projectID := createProject(t, pool, tokenA, "Acme Internal")

	body, _ := json.Marshal(map[string]string{"name": "Hijacked"})
	req := httptest.NewRequest(http.MethodPatch, "/projects/"+projectID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tokenB)
	req.SetPathValue("id", projectID)
	rec := httptest.NewRecorder()
	requireAuth(pool, updateProjectHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 updating another org's project, got %d", rec.Code)
	}
}

func TestDeleteProjectReturns404ForOtherOrg(t *testing.T) {
	pool := setupTestDB(t)
	tokenA := signupAndLogin(t, pool, "owner@acme.test")
	tokenB := signupAndLogin(t, pool, "owner@other.test")
	projectID := createProject(t, pool, tokenA, "Acme Internal")

	req := httptest.NewRequest(http.MethodDelete, "/projects/"+projectID, nil)
	req.Header.Set("Authorization", "Bearer "+tokenB)
	req.SetPathValue("id", projectID)
	rec := httptest.NewRecorder()
	requireAuth(pool, deleteProjectHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting another org's project, got %d", rec.Code)
	}
}

func TestDeleteProjectSucceedsForOwningOrg(t *testing.T) {
	pool := setupTestDB(t)
	token := signupAndLogin(t, pool, "owner@acme.test")
	projectID := createProject(t, pool, token, "Acme Internal")

	req := httptest.NewRequest(http.MethodDelete, "/projects/"+projectID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", projectID)
	rec := httptest.NewRecorder()
	requireAuth(pool, deleteProjectHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}
