package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

type Config struct {
	Pool                *pgxpool.Pool
	StripeSecretKey     string
	StripePriceID       string
	StripeWebhookSecret string
}

func newServer(cfg Config) http.Handler {
	pool := cfg.Pool
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", healthHandler)

	// Helper to register a route with CORS
	registerWithCORS := func(method, path string, handler http.Handler) {
		h := corsMiddleware(handler)
		mux.Handle(method+" "+path, h)
	}

	// Helper to register OPTIONS handler
	registerOptions := func(path string) {
		corsOnlyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if allowedOrigins[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			}
			w.WriteHeader(http.StatusNoContent)
		})
		mux.Handle("OPTIONS "+path, corsOnlyHandler)
	}

	// Auth routes
	registerWithCORS("POST", "/auth/signup", signupHandler(pool))
	registerWithCORS("POST", "/auth/login", loginHandler(pool))
	registerWithCORS("POST", "/auth/logout", logoutHandler(pool))
	registerWithCORS("GET", "/me", requireAuth(pool, meHandler(pool)))

	// Member routes
	registerWithCORS("GET", "/orgs/current/members", requireAuth(pool, listMembersHandler(pool)))
	registerWithCORS("POST", "/orgs/current/members", requireAuth(pool, addMemberHandler(pool)))
	registerWithCORS("PATCH", "/orgs/current/members/{userId}", requireAuth(pool, updateMemberRoleHandler(pool)))
	registerWithCORS("DELETE", "/orgs/current/members/{userId}", requireAuth(pool, removeMemberHandler(pool)))

	// Project routes
	registerWithCORS("GET", "/projects", requireAuth(pool, listProjectsHandler(pool)))
	registerWithCORS("POST", "/projects", requireAuth(pool, createProjectHandler(pool)))
	registerWithCORS("GET", "/projects/{id}", requireAuth(pool, getProjectHandler(pool)))
	registerWithCORS("PATCH", "/projects/{id}", requireAuth(pool, updateProjectHandler(pool)))
	registerWithCORS("DELETE", "/projects/{id}", requireAuth(pool, deleteProjectHandler(pool)))

	// Task routes
	registerWithCORS("GET", "/projects/{id}/tasks", requireAuth(pool, listTasksHandler(pool)))
	registerWithCORS("POST", "/projects/{id}/tasks", requireAuth(pool, createTaskHandler(pool)))
	registerWithCORS("PATCH", "/tasks/{id}", requireAuth(pool, updateTaskHandler(pool)))
	registerWithCORS("DELETE", "/tasks/{id}", requireAuth(pool, deleteTaskHandler(pool)))

	// Billing routes
	registerWithCORS("POST", "/billing/checkout-session", requireAuth(pool, checkoutSessionHandler(cfg)))
	mux.HandleFunc("POST /billing/webhook", billingWebhookHandler(cfg))
	registerWithCORS("GET", "/billing/status", requireAuth(pool, billingStatusHandler(pool)))

	// Register OPTIONS handlers for all CORS-enabled routes
	corsEnabledPaths := []string{
		"/auth/signup", "/auth/login", "/auth/logout", "/me",
		"/orgs/current/members", "/orgs/current/members/{userId}",
		"/projects", "/projects/{id}", "/projects/{id}/tasks", "/tasks/{id}",
		"/billing/checkout-session", "/billing/status",
	}
	for _, path := range corsEnabledPaths {
		registerOptions(path)
	}

	return mux
}

const startupTimeout = 10 * time.Second

func main() {
	startupCtx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()

	pool, err := newPool(startupCtx)
	if err != nil {
		log.Fatalf("db connection failed: %v", err)
	}
	defer pool.Close()

	cfg := Config{
		Pool:                pool,
		StripeSecretKey:     os.Getenv("STRIPE_SECRET_KEY"),
		StripePriceID:       os.Getenv("STRIPE_PRICE_ID"),
		StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
	}

	server := &http.Server{
		Addr:              ":8080",
		Handler:           newServer(cfg),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Println("saas-starter listening on :8080")
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
