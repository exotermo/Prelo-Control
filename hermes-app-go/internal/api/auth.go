package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/exotermo/hermes-app-go/internal/config"
)

type authContextKey struct{}

// AuthContext is the authenticated machine identity propagated to handlers. Tenant and
// integration are claims, never values accepted from request headers or bodies.
type AuthContext struct {
	Subject       string
	TenantID      string
	IntegrationID string
	TokenUse      string
	Scopes        map[string]struct{}
	RequestID     string
}

func (a AuthContext) HasScope(scope string) bool {
	_, ok := a.Scopes[scope]
	return ok
}

func FromContext(ctx context.Context) (AuthContext, bool) {
	value, ok := ctx.Value(authContextKey{}).(AuthContext)
	return value, ok
}

// JWTAuthMiddleware is the outer API gate. PermissionPolicy remains authoritative for tools;
// this middleware only authenticates the caller and limits which HTTP capability it may request.
type JWTAuthMiddleware struct {
	secret    []byte
	issuer    string
	audience  string
	clockSkew time.Duration
}

func NewJWTAuthMiddleware(cfg config.APIAuthConfig) (*JWTAuthMiddleware, error) {
	if strings.TrimSpace(cfg.Secret) == "" {
		return nil, &AuthConfigError{Message: "HERMES_GO_API_JWT_SECRET is required when API authentication is enabled"}
	}
	if strings.TrimSpace(cfg.Issuer) == "" || strings.TrimSpace(cfg.Audience) == "" {
		return nil, &AuthConfigError{Message: "API JWT issuer and audience are required"}
	}
	skew := cfg.ClockSkewSeconds
	if skew < 0 || skew > 300 {
		return nil, &AuthConfigError{Message: "API JWT clock skew must be between 0 and 300 seconds"}
	}
	return &JWTAuthMiddleware{secret: []byte(cfg.Secret), issuer: cfg.Issuer, audience: cfg.Audience, clockSkew: time.Duration(skew) * time.Second}, nil
}

type AuthConfigError struct{ Message string }

func (e *AuthConfigError) Error() string { return e.Message }

func (m *JWTAuthMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || r.URL.Path == "/actuator/health" {
			next.ServeHTTP(w, r)
			return
		}
		requestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if requestID == "" || len(requestID) > 128 {
			requestID = uuid.NewString()
		}
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		ctx = context.WithValue(ctx, authContextKey{}, AuthContext{RequestID: requestID})

		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if len(header) < 8 || !strings.EqualFold(header[:7], "Bearer ") {
			writeAuthError(w, http.StatusUnauthorized, "missing or malformed bearer token")
			return
		}
		token, err := m.parse(header[7:])
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			writeAuthError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		if issuer, err := claims.GetIssuer(); err != nil || issuer != m.issuer {
			writeAuthError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		if audiences, err := claims.GetAudience(); err != nil || !contains(audiences, m.audience) {
			writeAuthError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		if expiresAt, err := claims.GetExpirationTime(); err != nil || expiresAt == nil {
			writeAuthError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		if issuedAt, err := claims.GetIssuedAt(); err != nil || (issuedAt != nil && issuedAt.After(time.Now().Add(m.clockSkew))) {
			writeAuthError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		identity, err := identityFromClaims(claims, requestID)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		required := requiredScope(r.Method, r.URL.Path)
		if required != "" && !identity.HasScope(required) {
			writeAuthError(w, http.StatusForbidden, "the token does not grant this operation")
			return
		}
		ctx = context.WithValue(ctx, authContextKey{}, identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *JWTAuthMiddleware) parse(raw string) (*jwt.Token, error) {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithLeeway(m.clockSkew))
	return parser.ParseWithClaims(raw, jwt.MapClaims{}, func(token *jwt.Token) (any, error) {
		// Check the concrete method as well as the parser allowlist to prevent algorithm confusion.
		if token.Method != jwt.SigningMethodHS256 {
			return nil, jwt.ErrSignatureInvalid
		}
		return m.secret, nil
	})
}

func identityFromClaims(claims jwt.MapClaims, requestID string) (AuthContext, error) {
	sub, ok := claims["sub"].(string)
	if !ok || strings.TrimSpace(sub) == "" {
		return AuthContext{}, jwt.ErrTokenInvalidClaims
	}
	tenant, ok := claims["tenant_id"].(string)
	if !ok || strings.TrimSpace(tenant) == "" {
		return AuthContext{}, jwt.ErrTokenInvalidClaims
	}
	if _, err := uuid.Parse(tenant); err != nil {
		return AuthContext{}, jwt.ErrTokenInvalidClaims
	}
	tokenUse, _ := claims["token_use"].(string)
	if tokenUse != "integration" && tokenUse != "technical" {
		return AuthContext{}, jwt.ErrTokenInvalidClaims
	}
	scopes := parseScopes(claims["scope"])
	if len(scopes) == 0 {
		return AuthContext{}, jwt.ErrTokenInvalidClaims
	}
	integration, _ := claims["integration_id"].(string)
	if tokenUse == "integration" && strings.TrimSpace(integration) == "" {
		return AuthContext{}, jwt.ErrTokenInvalidClaims
	}
	return AuthContext{Subject: sub, TenantID: tenant, IntegrationID: integration, TokenUse: tokenUse, Scopes: scopes, RequestID: requestID}, nil
}

func parseScopes(value any) map[string]struct{} {
	result := map[string]struct{}{}
	switch scopes := value.(type) {
	case string:
		for _, scope := range strings.Fields(scopes) {
			if scope != "" {
				result[scope] = struct{}{}
			}
		}
	case []any:
		for _, raw := range scopes {
			if scope, ok := raw.(string); ok && scope != "" {
				result[scope] = struct{}{}
			}
		}
	case []string:
		for _, scope := range scopes {
			if scope != "" {
				result[scope] = struct{}{}
			}
		}
	}
	return result
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

type requestIDKey struct{}

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func requiredScope(method, path string) string {
	if method == http.MethodGet {
		switch {
		case strings.HasPrefix(path, "/api/v1/tools"):
			return "tools:read"
		case strings.HasPrefix(path, "/api/v1/approvals"):
			return "approvals:read"
		case strings.Contains(path, "/turns") || strings.HasSuffix(path, "/tree"):
			return "observability:read"
		case strings.HasPrefix(path, "/api/v1/tasks"):
			return "tasks:read"
		}
	}
	if method == http.MethodPost {
		switch {
		case path == "/api/v1/tasks":
			return "tasks:create"
		case strings.HasSuffix(path, "/execute"):
			return "tasks:execute"
		case path == "/api/v1/hermes/chat":
			return "chat:use"
		case strings.Contains(path, "/tools/"):
			return "tools:invoke"
		case strings.HasSuffix(path, "/approve") || strings.HasSuffix(path, "/deny"):
			return "approvals:decide"
		}
	}
	return ""
}

func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="hermes-api"`)
	writeJSON(w, status, errorResponse{Code: map[int]string{401: "unauthorized", 403: "forbidden"}[status], Message: message})
}
