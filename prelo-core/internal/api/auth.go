package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/config"
	"github.com/exotermo/prelo-core/internal/domain"
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
	// ProjectID is resolved (Fase W) from the X-Project-Id header, only for project-scoped
	// paths (see projectScopedPath) — nil means "no project selected", the pre-Fase-W
	// "unassigned" bucket. Unlike TenantID, this is never a JWT claim: the same dashboard
	// session moves between several projects across requests.
	ProjectID *uuid.UUID
}

func (a AuthContext) HasScope(scope string) bool {
	_, ok := a.Scopes[scope]
	return ok
}

func FromContext(ctx context.Context) (AuthContext, bool) {
	value, ok := ctx.Value(authContextKey{}).(AuthContext)
	return value, ok
}

// projectFinder and projectMembershipChecker are the narrow ports JWTAuthMiddleware needs to
// resolve Fase W's access boundary — kept as interfaces (not application.ProjectRepository/
// ProjectMemberRepository directly) so the middleware depends on exactly the two calls it makes,
// nothing else. They're satisfied by two different concrete repositories (projects vs.
// project_members are separate tables), so the middleware takes both separately.
type projectFinder interface {
	FindByID(ctx context.Context, id domain.ProjectID) (domain.Project, error)
}

type projectMembershipChecker interface {
	IsMember(ctx context.Context, projectID domain.ProjectID, userID domain.DashboardUserID) (bool, error)
}

// JWTAuthMiddleware is the outer API gate. PermissionPolicy remains authoritative for tools;
// this middleware only authenticates the caller and limits which HTTP capability it may request.
type JWTAuthMiddleware struct {
	secret    []byte
	issuer    string
	audience  string
	clockSkew time.Duration
	projects  projectFinder
	members   projectMembershipChecker
	apiKeys   ApiKeyAuthenticator
}

func NewJWTAuthMiddleware(cfg config.APIAuthConfig, projects projectFinder, members projectMembershipChecker, apiKeys ApiKeyAuthenticator) (*JWTAuthMiddleware, error) {
	if strings.TrimSpace(cfg.Secret) == "" {
		return nil, &AuthConfigError{Message: "PRELO_API_JWT_SECRET is required when API authentication is enabled"}
	}
	if strings.TrimSpace(cfg.Issuer) == "" || strings.TrimSpace(cfg.Audience) == "" {
		return nil, &AuthConfigError{Message: "API JWT issuer and audience are required"}
	}
	skew := cfg.ClockSkewSeconds
	if skew < 0 || skew > 300 {
		return nil, &AuthConfigError{Message: "API JWT clock skew must be between 0 and 300 seconds"}
	}
	return &JWTAuthMiddleware{secret: []byte(cfg.Secret), issuer: cfg.Issuer, audience: cfg.Audience, clockSkew: time.Duration(skew) * time.Second, projects: projects, members: members, apiKeys: apiKeys}, nil
}

type AuthConfigError struct{ Message string }

func (e *AuthConfigError) Error() string { return e.Message }

const (
	apiKeyTokenPrefix = "prl_"
	tokenUseApiKey    = "api_key"
)

// ApiKeyAuthenticator resolves a raw Prelo-issued API key (Fase I) — satisfied by
// application.IntegrationService. nil when integrations aren't configured: every "prl_" bearer
// is then simply rejected.
type ApiKeyAuthenticator interface {
	AuthenticateApiKey(ctx context.Context, raw string) (domain.ApiKey, error)
}

func (m *JWTAuthMiddleware) apiKeyIdentity(ctx context.Context, raw, requestID string) (AuthContext, error) {
	if m.apiKeys == nil {
		return AuthContext{}, jwt.ErrTokenInvalidClaims
	}
	key, err := m.apiKeys.AuthenticateApiKey(ctx, raw)
	if err != nil {
		return AuthContext{}, err
	}
	scopes := map[string]struct{}{}
	for _, s := range key.Scopes {
		scopes[s] = struct{}{}
	}
	projectID := key.ProjectID.Value
	return AuthContext{Subject: "api_key:" + key.ID.String(), TokenUse: tokenUseApiKey, Scopes: scopes,
		RequestID: requestID, ProjectID: &projectID}, nil
}

func (m *JWTAuthMiddleware) jwtIdentity(raw, requestID string) (AuthContext, error) {
	token, err := m.parse(raw)
	if err != nil {
		return AuthContext{}, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return AuthContext{}, jwt.ErrTokenInvalidClaims
	}
	if issuer, err := claims.GetIssuer(); err != nil || issuer != m.issuer {
		return AuthContext{}, jwt.ErrTokenInvalidIssuer
	}
	if audiences, err := claims.GetAudience(); err != nil || !contains(audiences, m.audience) {
		return AuthContext{}, jwt.ErrTokenInvalidAudience
	}
	if expiresAt, err := claims.GetExpirationTime(); err != nil || expiresAt == nil {
		return AuthContext{}, jwt.ErrTokenInvalidClaims
	}
	if issuedAt, err := claims.GetIssuedAt(); err != nil || (issuedAt != nil && issuedAt.After(time.Now().Add(m.clockSkew))) {
		return AuthContext{}, jwt.ErrTokenUsedBeforeIssued
	}
	return identityFromClaims(claims, requestID)
}

func (m *JWTAuthMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || r.URL.Path == "/actuator/health" || isPublicDashboardAuthPath(r.URL.Path) {
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
		raw := strings.TrimSpace(header[7:])
		var identity AuthContext
		var err error
		if strings.HasPrefix(raw, apiKeyTokenPrefix) {
			identity, err = m.apiKeyIdentity(r.Context(), raw, requestID)
		} else {
			identity, err = m.jwtIdentity(raw, requestID)
		}
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		required := requiredScope(r.Method, r.URL.Path)
		if required != "" && !identity.HasScope(required) {
			writeAuthError(w, http.StatusForbidden, "the token does not grant this operation")
			return
		}
		if identity.TokenUse == tokenUseApiKey {
			// An API key's project is fixed by the key itself; a header naming any other project
			// is refused rather than silently ignored, so a misconfigured client finds out.
			if header := strings.TrimSpace(r.Header.Get("X-Project-Id")); header != "" && header != identity.ProjectID.String() {
				writeAuthError(w, http.StatusForbidden, "the token does not grant this operation")
				return
			}
		} else if projectScopedPath(r.URL.Path) {
			resolved, ok, status, message := m.resolveProject(r.Context(), r.Header.Get("X-Project-Id"), identity)
			if !ok {
				writeAuthError(w, status, message)
				return
			}
			identity.ProjectID = resolved
		}
		ctx = context.WithValue(ctx, authContextKey{}, identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// projectScopedPath marks the routes Fase W's access boundary applies to — everything derived
// from Task (Tasks itself, Pipeline, Approvals) plus Servers. Tools/Chat/Settings/Integrações/
// Usuários/Projetos stay global, untouched by X-Project-Id.
func projectScopedPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/tasks") ||
		strings.HasPrefix(path, "/api/v1/servers") ||
		strings.HasPrefix(path, "/api/v1/pipeline") ||
		strings.HasPrefix(path, "/api/v1/approvals") ||
		strings.HasPrefix(path, "/api/v1/integrations")
}

// resolveProject implements Fase W's central access rule. An empty header resolves to nil (the
// "unassigned" bucket) for everyone, no membership check needed. A non-empty header must name an
// existing, non-deleted project; a caller without projects:manage (i.e. OPERATOR) must also be a
// member of it, checked via m.projects.IsMember — ADMIN (has projects:manage) always passes.
func (m *JWTAuthMiddleware) resolveProject(ctx context.Context, rawProjectID string, identity AuthContext) (*uuid.UUID, bool, int, string) {
	rawProjectID = strings.TrimSpace(rawProjectID)
	if rawProjectID == "" {
		return nil, true, 0, ""
	}
	projectID, err := uuid.Parse(rawProjectID)
	if err != nil {
		return nil, false, http.StatusBadRequest, "X-Project-Id must be a valid UUID"
	}
	project, err := m.projects.FindByID(ctx, domain.ProjectID{Value: projectID})
	if err != nil {
		if errors.Is(err, application.ErrProjectNotFound) {
			return nil, false, http.StatusForbidden, "the token does not grant this operation"
		}
		return nil, false, http.StatusInternalServerError, "could not resolve project"
	}
	if !identity.HasScope("projects:manage") {
		userID, err := uuid.Parse(identity.Subject)
		if err != nil {
			return nil, false, http.StatusForbidden, "the token does not grant this operation"
		}
		isMember, err := m.members.IsMember(ctx, project.ID, domain.DashboardUserID{Value: userID})
		if err != nil {
			return nil, false, http.StatusInternalServerError, "could not resolve project"
		}
		if !isMember {
			return nil, false, http.StatusForbidden, "the token does not grant this operation"
		}
	}
	return &project.ID.Value, true, 0, ""
}

// projectIdentity extracts the project resolved by JWTAuthMiddleware for the current request —
// nil means "no project selected" (the unassigned bucket), mirroring tenantIdentity's shape.
func projectIdentity(ctx context.Context) *uuid.UUID {
	identity, ok := FromContext(ctx)
	if !ok {
		return nil
	}
	return identity.ProjectID
}

// isPublicDashboardAuthPath carves out the routes a human has no JWT yet to call: login,
// activation, password reset and the admin-token-gated bootstrap of the very first user
// (DashboardAuthHandler / DashboardBootstrapHandler enforce their own checks — rate limiting,
// one-time tokens, X-Admin-Token — this middleware just steps aside for them).
func isPublicDashboardAuthPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/dashboard-auth/") || path == "/api/v1/admin/dashboard-invitations"
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
	tokenUse, _ := claims["token_use"].(string)
	if tokenUse != "integration" && tokenUse != "technical" && tokenUse != "dashboard" {
		return AuthContext{}, jwt.ErrTokenInvalidClaims
	}
	scopes := parseScopes(claims["scope"])
	if len(scopes) == 0 {
		return AuthContext{}, jwt.ErrTokenInvalidClaims
	}
	// A dashboard session is a human operator of this single Prelo instance, not scoped to any
	// tenant — every other token_use is machine-to-machine and always carries a tenant_id.
	if tokenUse == "dashboard" {
		return AuthContext{Subject: sub, TokenUse: tokenUse, Scopes: scopes, RequestID: requestID}, nil
	}
	tenant, ok := claims["tenant_id"].(string)
	if !ok || strings.TrimSpace(tenant) == "" {
		return AuthContext{}, jwt.ErrTokenInvalidClaims
	}
	if _, err := uuid.Parse(tenant); err != nil {
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
	// Fase C1: clients (CRM), search and the home screen. Reading is clients:read; editing is
	// clients:manage (ADMIN and OPERATOR — day-to-day work); deleting a client is ADMIN-only.
	if strings.HasPrefix(path, "/api/v1/clients") || path == "/api/v1/search" || path == "/api/v1/home" || path == "/api/v1/recent" {
		switch {
		case method == http.MethodGet || path == "/api/v1/recent":
			return "clients:read"
		case method == http.MethodDelete && strings.Count(strings.TrimPrefix(path, "/api/v1/clients/"), "/") == 0:
			return "clients:delete"
		default:
			return "clients:manage"
		}
	}
	// Fase I: every integrations route (reading included — the list shows key prefixes and
	// webhook URLs) is ADMIN-only. An API key never carries this scope, so a key can't mint keys.
	if strings.HasPrefix(path, "/api/v1/integrations") {
		return "integrations:manage"
	}
	// Fase PA: project files — any project member reads and uploads (membership and the
	// delete rule are checked in ProjectFileHandler), so this is projects:read for every method.
	if strings.HasPrefix(path, "/api/v1/projects/") && strings.Contains(path, "/files") {
		return "projects:read"
	}
	// Fase M: the instance-wide model connection is instance configuration (ADMIN).
	if strings.HasPrefix(path, "/api/v1/model-connections") {
		return "settings:manage"
	}
	if method == http.MethodGet {
		switch {
		case strings.HasPrefix(path, "/api/v1/tools"):
			return "tools:read"
		case strings.HasPrefix(path, "/api/v1/approvals"):
			return "approvals:read"
		case strings.Contains(path, "/turns") || strings.HasSuffix(path, "/tree") || strings.HasPrefix(path, "/api/v1/pipeline"):
			return "observability:read"
		case strings.HasPrefix(path, "/api/v1/tasks"):
			return "tasks:read"
		case strings.HasPrefix(path, "/api/v1/settings"):
			return "settings:manage"
		case strings.HasPrefix(path, "/api/v1/users"):
			return "users:manage"
		case strings.HasPrefix(path, "/api/v1/servers"):
			return "servers:read"
		// Membership is projects:manage even on GET (ADMIN-only in this first version) —
		// checked before the generic /api/v1/projects* -> projects:read fallback below.
		case strings.HasPrefix(path, "/api/v1/projects") && strings.Contains(path, "/members"):
			return "projects:manage"
		case strings.HasPrefix(path, "/api/v1/projects"):
			return "projects:read"
		}
	}
	if method == http.MethodPatch && strings.HasPrefix(path, "/api/v1/projects") {
		return "projects:manage"
	}
	if method == http.MethodGet && strings.HasPrefix(path, "/api/v1/agents") {
		return "tasks:read"
	}
	if method == http.MethodPut {
		switch {
		case strings.HasPrefix(path, "/api/v1/projects"):
			return "projects:manage"
		case strings.HasPrefix(path, "/api/v1/settings"):
			return "settings:manage"
		case strings.HasPrefix(path, "/api/v1/users"):
			return "users:manage"
		}
	}
	if method == http.MethodPost {
		switch {
		case path == "/api/v1/tasks":
			return "tasks:create"
		case strings.HasSuffix(path, "/execute"):
			return "tasks:execute"
		case path == "/api/v1/chat":
			return "chat:use"
		case strings.Contains(path, "/tools/"):
			return "tools:invoke"
		case strings.HasSuffix(path, "/approve") || strings.HasSuffix(path, "/deny"):
			return "approvals:decide"
		case path == "/api/v1/users":
			return "users:manage"
		// A health-check is a read trigger, not a cadastro change — gated like GET /servers.
		case strings.HasSuffix(path, "/health-check"):
			return "servers:read"
		case path == "/api/v1/servers":
			return "servers:manage"
		case strings.HasPrefix(path, "/api/v1/projects"):
			return "projects:manage"
		}
	}
	if method == http.MethodDelete {
		switch {
		case strings.HasPrefix(path, "/api/v1/servers"):
			return "servers:manage"
		case strings.HasPrefix(path, "/api/v1/projects"):
			return "projects:manage"
		}
	}
	return ""
}

func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="prelo-api"`)
	writeJSON(w, status, errorResponse{Code: map[int]string{401: "unauthorized", 403: "forbidden"}[status], Message: message})
}
