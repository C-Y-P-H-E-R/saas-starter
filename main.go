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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	return mux
}

func main() {
	ctx := context.Background()
	pool, err := newPool(ctx)
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
