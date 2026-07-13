package main

import "net/http"

var allowedOrigins = map[string]bool{
	"https://portfolio-site-gold-alpha.vercel.app": true,
	"http://localhost:3000":                        true,
}

func setCORSHeaders(w http.ResponseWriter, origin string) {
	if allowedOrigins[origin] {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w, r.Header.Get("Origin"))
		// OPTIONS never reaches here in practice: every route registered via
		// newServer's registerWithCORS gets its own dedicated "OPTIONS <path>"
		// mux registration (main.go) — Go's ServeMux exact-method patterns
		// ("POST /x") never match an OPTIONS request on their own. This
		// branch is defense-in-depth for any handler wrapped in
		// corsMiddleware directly, outside that helper.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
