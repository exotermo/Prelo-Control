package platform

import "net/http"

// CORS wraps a handler to allow the dashboard SPA (hermes-dashboard, a separate origin/port —
// same story as messaging-core's own CORS addition for messager-dashboard) to call this API
// straight from the browser.
//
// Fase G1 made this a credentialed policy: the dashboard-auth refresh/logout calls send
// credentials: "include" so the browser attaches the HttpOnly session cookie. Per the Fetch
// spec, a credentialed request can never be paired with Access-Control-Allow-Origin: "*" — the
// browser silently blocks the response ("NetworkError when attempting to fetch resource" is
// exactly this). So allowedOrigins must be an explicit list, echoed back only when it matches,
// with Access-Control-Allow-Credentials: true alongside it.
func CORS(next http.Handler, allowedOrigins []string) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if _, ok := allowed[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-Id, X-Dashboard-Request, X-Admin-Token, X-Project-Id")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
