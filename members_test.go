package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func addMember(t *testing.T, pool *pgxpool.Pool, ownerToken, email, role string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "role": role})
	req := httptest.NewRequest(http.MethodPost, "/orgs/current/members", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	rec := httptest.NewRecorder()
	requireAuth(pool, addMemberHandler(pool)).ServeHTTP(rec, req)
	return rec
}

func TestAddMemberRequiresExistingAccount(t *testing.T) {
	pool := setupTestDB(t)
	ownerToken := signupAndLogin(t, pool, "owner@acme.test")

	rec := addMember(t, pool, ownerToken, "nobody@nowhere.test", "member")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-existent invitee, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddMemberSucceedsForExistingUser(t *testing.T) {
	pool := setupTestDB(t)
	ownerToken := signupAndLogin(t, pool, "owner@acme.test")
	signupAndLogin(t, pool, "member@other.test") // creates the account, in their own org

	rec := addMember(t, pool, ownerToken, "member@other.test", "member")

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAddMemberRejectsNonAdminCaller(t *testing.T) {
	pool := setupTestDB(t)
	ownerToken := signupAndLogin(t, pool, "owner@acme.test")
	memberToken := signupAndLogin(t, pool, "member@other.test")
	addMember(t, pool, ownerToken, "member@other.test", "member")

	// Create a new session for memberToken in org1 (where they're a member, not an owner)
	var ownerID, memberID, orgID string
	ctx := context.Background()
	pool.QueryRow(ctx, "SELECT id FROM users WHERE email = $1", "owner@acme.test").Scan(&ownerID)
	pool.QueryRow(ctx, "SELECT id FROM users WHERE email = $1", "member@other.test").Scan(&memberID)
	pool.QueryRow(ctx, "SELECT org_id FROM memberships WHERE user_id = $1 AND role = 'owner'", ownerID).Scan(&orgID)

	newToken := newID() + newID()
	pool.Exec(ctx, "INSERT INTO sessions (token, user_id, org_id, expires_at) VALUES ($1, $2, $3, now() + interval '7 days')", newToken, memberID, orgID)
	memberToken = newToken

	body, _ := json.Marshal(map[string]string{"email": "third@else.test", "role": "member"})
	req := httptest.NewRequest(http.MethodPost, "/orgs/current/members", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+memberToken)
	rec := httptest.NewRecorder()
	requireAuth(pool, addMemberHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for member-role caller, got %d", rec.Code)
	}
}

func TestListMembersReturnsAllOrgMembers(t *testing.T) {
	pool := setupTestDB(t)
	ownerToken := signupAndLogin(t, pool, "owner@acme.test")
	signupAndLogin(t, pool, "member@other.test")
	addMember(t, pool, ownerToken, "member@other.test", "member")

	req := httptest.NewRequest(http.MethodGet, "/orgs/current/members", nil)
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	rec := httptest.NewRecorder()
	requireAuth(pool, listMembersHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var members []struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	json.Unmarshal(rec.Body.Bytes(), &members)
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}
}

func TestUpdateMemberRoleChangesRole(t *testing.T) {
	pool := setupTestDB(t)
	ownerToken := signupAndLogin(t, pool, "owner@acme.test")
	signupAndLogin(t, pool, "member@other.test")
	addMember(t, pool, ownerToken, "member@other.test", "member")

	var memberID string
	pool.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", "member@other.test").Scan(&memberID)

	body, _ := json.Marshal(map[string]string{"role": "admin"})
	req := httptest.NewRequest(http.MethodPatch, "/orgs/current/members/"+memberID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	req.SetPathValue("userId", memberID)
	rec := httptest.NewRecorder()
	requireAuth(pool, updateMemberRoleHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateMemberRoleBlocksDemotingLastOwner(t *testing.T) {
	pool := setupTestDB(t)
	ownerToken := signupAndLogin(t, pool, "owner@acme.test")

	var ownerID string
	pool.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", "owner@acme.test").Scan(&ownerID)

	body, _ := json.Marshal(map[string]string{"role": "member"})
	req := httptest.NewRequest(http.MethodPatch, "/orgs/current/members/"+ownerID, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	req.SetPathValue("userId", ownerID)
	rec := httptest.NewRecorder()
	requireAuth(pool, updateMemberRoleHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 blocking last-owner demotion, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRemoveMemberBlocksRemovingLastOwner(t *testing.T) {
	pool := setupTestDB(t)
	ownerToken := signupAndLogin(t, pool, "owner@acme.test")

	var ownerID string
	pool.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", "owner@acme.test").Scan(&ownerID)

	req := httptest.NewRequest(http.MethodDelete, "/orgs/current/members/"+ownerID, nil)
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	req.SetPathValue("userId", ownerID)
	rec := httptest.NewRecorder()
	requireAuth(pool, removeMemberHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 blocking last-owner removal, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRemoveMemberSucceedsForNonLastOwner(t *testing.T) {
	pool := setupTestDB(t)
	ownerToken := signupAndLogin(t, pool, "owner@acme.test")
	signupAndLogin(t, pool, "member@other.test")
	addMember(t, pool, ownerToken, "member@other.test", "member")

	var memberID string
	pool.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", "member@other.test").Scan(&memberID)

	req := httptest.NewRequest(http.MethodDelete, "/orgs/current/members/"+memberID, nil)
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	req.SetPathValue("userId", memberID)
	rec := httptest.NewRecorder()
	requireAuth(pool, removeMemberHandler(pool)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}
