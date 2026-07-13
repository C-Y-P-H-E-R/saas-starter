package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/customer"
	"github.com/stripe/stripe-go/v81/webhook"
)

const frontendBaseURL = "https://portfolio-site-gold-alpha.vercel.app"

func checkoutSessionHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessionFromContext(r.Context())
		if !isAdminOrOwner(s.Role) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		stripe.Key = cfg.StripeSecretKey
		ctx := r.Context()

		var customerID string
		cfg.Pool.QueryRow(ctx, "SELECT COALESCE(stripe_customer_id, '') FROM orgs WHERE id = $1", s.OrgID).Scan(&customerID)

		if customerID == "" {
			cus, err := customer.New(&stripe.CustomerParams{})
			if err != nil {
				http.Error(w, "could not create stripe customer", http.StatusInternalServerError)
				return
			}
			customerID = cus.ID
			cfg.Pool.Exec(ctx, "UPDATE orgs SET stripe_customer_id = $1 WHERE id = $2", customerID, s.OrgID)
		}

		params := &stripe.CheckoutSessionParams{
			Mode:     stripe.String(string(stripe.CheckoutSessionModeSubscription)),
			Customer: stripe.String(customerID),
			LineItems: []*stripe.CheckoutSessionLineItemParams{
				{Price: stripe.String(cfg.StripePriceID), Quantity: stripe.Int64(1)},
			},
			SuccessURL: stripe.String(frontendBaseURL + "/app/saas-starter/billing?success=1"),
			CancelURL:  stripe.String(frontendBaseURL + "/app/saas-starter/billing"),
		}

		sess, err := session.New(params)
		if err != nil {
			http.Error(w, "could not create checkout session", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"url": sess.URL})
	}
}

func billingWebhookHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "cannot read body", http.StatusBadRequest)
			return
		}

		event, err := webhook.ConstructEvent(body, r.Header.Get("Stripe-Signature"), cfg.StripeWebhookSecret)
		if err != nil {
			http.Error(w, "invalid signature", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		tag, err := cfg.Pool.Exec(ctx, "INSERT INTO processed_stripe_events (id) VALUES ($1) ON CONFLICT (id) DO NOTHING", event.ID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if tag.RowsAffected() == 0 {
			w.WriteHeader(http.StatusOK)
			return
		}

		switch event.Type {
		case "checkout.session.completed":
			var sess stripe.CheckoutSession
			if err := json.Unmarshal(event.Data.Raw, &sess); err == nil {
				if _, err := cfg.Pool.Exec(ctx, "UPDATE orgs SET plan = 'pro', stripe_subscription_id = $1 WHERE stripe_customer_id = $2",
					sess.Subscription.ID, sess.Customer.ID); err != nil {
					log.Printf("billing webhook: failed to apply checkout.session.completed for event %s: %v", event.ID, err)
				}
			}
		case "customer.subscription.updated":
			var sub stripe.Subscription
			if err := json.Unmarshal(event.Data.Raw, &sub); err == nil {
				plan := "free"
				if sub.Status == stripe.SubscriptionStatusActive || sub.Status == stripe.SubscriptionStatusTrialing {
					plan = "pro"
				}
				if _, err := cfg.Pool.Exec(ctx, "UPDATE orgs SET plan = $1, stripe_subscription_id = $2 WHERE stripe_customer_id = $3",
					plan, sub.ID, sub.Customer.ID); err != nil {
					log.Printf("billing webhook: failed to apply customer.subscription.updated for event %s: %v", event.ID, err)
				}
			}
		case "customer.subscription.deleted":
			var sub stripe.Subscription
			if err := json.Unmarshal(event.Data.Raw, &sub); err == nil {
				if _, err := cfg.Pool.Exec(ctx, "UPDATE orgs SET plan = 'free' WHERE stripe_customer_id = $1", sub.Customer.ID); err != nil {
					log.Printf("billing webhook: failed to apply customer.subscription.deleted for event %s: %v", event.ID, err)
				}
			}
		}

		w.WriteHeader(http.StatusOK)
	}
}

func billingStatusHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := sessionFromContext(r.Context())

		var plan string
		pool.QueryRow(r.Context(), "SELECT plan FROM orgs WHERE id = $1", s.OrgID).Scan(&plan)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"plan": plan})
	}
}
