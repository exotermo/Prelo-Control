package platform

import "net/http"

// CORS wraps a handler to allow the dashboard SPA (hermes-dashboard, a separate origin/port —
// same story as messaging-core's own CORS addition for messager-dashboard) to call this API
// straight from the browser. hermes-go has no auth today, so this is deliberately permissive
// (no credentials, no cookie-based session to protect) — tightening this is part of the same
// "add an auth gate before exposing beyond localhost" evolution noted in ADR-014.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-Id")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
