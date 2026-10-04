package api

import (
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/exotermo/prelo-core/internal/config"
	"github.com/exotermo/prelo-core/internal/domain"
)

// dashboardAccessTTL mirrors messaging-core's dashboard access token: short-lived by design —
// the refresh cookie is what actually keeps a session alive across page loads.
const dashboardAccessTTL = 15 * time.Minute

// dashboardOperatorScopes covers day-to-day agent operation — tasks, approvals, tools, chat,
// observability, and viewing server health (Fase S1) — everything a DashboardRoleOperator
// session can do.
var dashboardOperatorScopes = []string{
	"tasks:create", "tasks:read", "tasks:execute", "chat:use",
	"tools:read", "tools:invoke", "approvals:read", "approvals:decide",
	"observability:read", "servers:read", "projects:read",
	"clients:read", "clients:manage",
}

// dashboardAdminScopes adds settings:manage (Integrações/Configurações, Fase G2), users:manage
// (invite/promote other dashboard users, Fase H), servers:manage (register/remove servers —
// holds SSH credentials, Fase S1), and projects:manage (create/delete projects and manage
// membership, Fase W — also what lets JWTAuthMiddleware.resolveProject skip the membership
// check entirely, see auth.go) on top of everything an operator can do — see domain.DashboardRole.
var dashboardAdminScopes = append(append([]string{}, dashboardOperatorScopes...), "settings:manage", "users:manage", "servers:manage", "projects:manage", "integrations:manage", "clients:delete")

func scopesFor(role domain.DashboardRole) []string {
	if role == domain.DashboardRoleAdmin {
		return dashboardAdminScopes
	}
	return dashboardOperatorScopes
}

// DashboardSessionIssuer mints token_use="dashboard" access tokens signed with the same
// secret/issuer/audience JWTAuthMiddleware already validates integration/technical tokens
// against — no new shared secret. identityFromClaims accepts "dashboard" as a third token_use
// and, only for it, does not require a tenant_id claim (a dashboard session isn't tenant-scoped).
type DashboardSessionIssuer struct {
	secret   []byte
	issuer   string
	audience string
}

func NewDashboardSessionIssuer(cfg config.APIAuthConfig) *DashboardSessionIssuer {
	return &DashboardSessionIssuer{secret: []byte(cfg.Secret), issuer: cfg.Issuer, audience: cfg.Audience}
}

func (i *DashboardSessionIssuer) IssueAccessToken(userID string, role domain.DashboardRole) (string, int, error) {
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"sub": userID, "token_use": "dashboard", "scope": scopesFor(role),
		"iss": i.issuer, "aud": i.audience, "iat": now.Unix(), "exp": now.Add(dashboardAccessTTL).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(i.secret)
	if err != nil {
		return "", 0, err
	}
	return signed, int(dashboardAccessTTL.Seconds()), nil
}
