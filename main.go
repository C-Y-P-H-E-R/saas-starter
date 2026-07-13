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

	// registerWithCORS registers a method+path route wrapped in corsMiddleware,
	// and — the first time this path is seen — also registers a standalone
	// "OPTIONS <path>" route for the CORS preflight. Go's ServeMux
	// exact-method patterns ("POST /x") never match an OPTIONS request on
	// their own, so without this, every preflight would 404 (or, for a path
	// already registered under a different method, 405) before corsMiddleware
	// ever ran. Auto-registering here — rather than maintaining a separate
	// list of CORS-enabled paths — makes it structurally impossible to add a
	// route and forget its OPTIONS handler.
	optionsRegistered := map[string]bool{}
	registerWithCORS := func(method, path string, handler http.Handler) {
		mux.Handle(method+" "+path, corsMiddleware(handler))
		if !optionsRegistered[path] {
			mux.HandleFunc("OPTIONS "+path, func(w http.ResponseWriter, r *http.Request) {
				setCORSHeaders(w, r.Header.Get("Origin"))
				w.WriteHeader(http.StatusNoContent)
			})
			optionsRegistered[path] = true
		}
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
