package gateway

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// serviceToken mints a fresh HS256 JWT per call — never cached — matching
// GatewayClient.serviceToken() exactly: sub=hermes-app, iss/aud from config, client_id,
// scope=["llm:invoke"], iat=now, exp=now+60s, jti=random uuid.
func serviceToken(secret, issuer, audience string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":       "hermes-app",
		"iss":       issuer,
		"aud":       audience,
		"client_id": "hermes-app",
		"scope":     []string{"llm:invoke"},
		"iat":       now.Unix(),
		"exp":       now.Add(60 * time.Second).Unix(),
		"jti":       uuid.NewString(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}
