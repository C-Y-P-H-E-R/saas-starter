package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/webhook"
)

const testWebhookSecret = "whsec_test_secret"

func TestBillingStatusReturnsFreeByDefault(t *testing.T) {
	pool := setupTestDB(t)
	token := signupAndLogin(t, pool, "owner@acme.test")

	req := httptest.NewRequest(http.MethodGet, "/billing/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	requireAuth(pool, billingStatusHandler(pool)).ServeHTTP(rec, req)

	var resp struct {
		Plan string `json:"plan"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Plan != "free" {
		t.Fatalf("expected default plan %q, got %q", "free", resp.Plan)
	}
}

func TestWebhookRejectsInvalidSignature(t *testing.T) {
	pool := setupTestDB(t)
	cfg := Config{Pool: pool, StripeWebhookSecret: testWebhookSecret}

	req := httptest.NewRequest(http.MethodPost, "/billing/webhook", bytes.NewReader([]byte(`{"bogus":true}`)))
	req.Header.Set("Stripe-Signature", "not-a-real-signature")
	rec := httptest.NewRecorder()
	billingWebhookHandler(cfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid signature, got %d", rec.Code)
	}
}

func TestWebhookCheckoutSessionCompletedUpgradesOrgToPro(t *testing.T) {
	pool := setupTestDB(t)
	token := signupAndLogin(t, pool, "owner@acme.test")

	var orgID string
	pool.QueryRow(context.Background(), "SELECT org_id FROM sessions WHERE token = $1", token).Scan(&orgID)
	pool.Exec(context.Background(), "UPDATE orgs SET stripe_customer_id = $1 WHERE id = $2", "cus_test123", orgID)

	// api_version must be on the same release train as stripe.APIVersion:
	// stripe-go v81's webhook.ConstructEvent rejects events whose api_version
	// train doesn't match the compiled-in SDK version, independent of signature
	// validity. See task-5-report.md for details.
	payload := []byte(fmt.Sprintf(`{
		"id": "evt_test_1",
		"type": "checkout.session.completed",
		"api_version": %q,
		"data": {"object": {"customer": "cus_test123", "subscription": "sub_test123"}}
	}`, stripe.APIVersion))
	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
		Payload: payload,
		Secret:  testWebhookSecret,
	})

	req := httptest.NewRequest(http.MethodPost, "/billing/webhook", bytes.NewReader(signed.Payload))
	req.Header.Set("Stripe-Signature", signed.Header)
	rec := httptest.NewRecorder()
	billingWebhookHandler(Config{Pool: pool, StripeWebhookSecret: testWebhookSecret}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var plan string
	pool.QueryRow(context.Background(), "SELECT plan FROM orgs WHERE id = $1", orgID).Scan(&plan)
	if plan != "pro" {
		t.Fatalf("expected plan %q after webhook, got %q", "pro", plan)
	}
}

func TestWebhookIsIdempotentOnDuplicateEventID(t *testing.T) {
	pool := setupTestDB(t)
	token := signupAndLogin(t, pool, "owner@acme.test")

	var orgID string
	pool.QueryRow(context.Background(), "SELECT org_id FROM sessions WHERE token = $1", token).Scan(&orgID)
	pool.Exec(context.Background(), "UPDATE orgs SET stripe_customer_id = $1 WHERE id = $2", "cus_test456", orgID)

	payload := []byte(fmt.Sprintf(`{
		"id": "evt_test_dup",
		"type": "checkout.session.completed",
		"api_version": %q,
		"data": {"object": {"customer": "cus_test456", "subscription": "sub_test456"}}
	}`, stripe.APIVersion))
	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
		Payload: payload,
		Secret:  testWebhookSecret,
	})
	cfg := Config{Pool: pool, StripeWebhookSecret: testWebhookSecret}

	req1 := httptest.NewRequest(http.MethodPost, "/billing/webhook", bytes.NewReader(signed.Payload))
	req1.Header.Set("Stripe-Signature", signed.Header)
	billingWebhookHandler(cfg).ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodPost, "/billing/webhook", bytes.NewReader(signed.Payload))
	req2.Header.Set("Stripe-Signature", signed.Header)
	rec2 := httptest.NewRecorder()
	billingWebhookHandler(cfg).ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on replayed event, got %d", rec2.Code)
	}

	var count int
	pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM processed_stripe_events WHERE id = $1", "evt_test_dup").Scan(&count)
	if count != 1 {
		t.Fatalf("expected exactly 1 processed-event row, got %d", count)
	}
}
