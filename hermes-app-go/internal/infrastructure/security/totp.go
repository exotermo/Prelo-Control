package security

import (
	"github.com/pquerna/otp/totp"
)

// TotpProvider wraps github.com/pquerna/otp (RFC 6238) — compatible with Google Authenticator
// and any other standard TOTP app.
type TotpProvider struct {
	issuer string
}

func NewTotpProvider(issuer string) *TotpProvider {
	return &TotpProvider{issuer: issuer}
}

// GenerateSecret returns a fresh base32 secret and its otpauth:// URI (for the QR code shown
// during setup) — nothing is persisted here, the caller encrypts and stores the secret only
// once the user confirms a valid code (ConfirmTotp), same "prove possession before enabling"
// flow as messaging-core's DashboardAuthService.setupTotp/confirmTotp.
func (p *TotpProvider) GenerateSecret(accountName string) (secret string, otpauthURI string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: p.issuer, AccountName: accountName})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.String(), nil
}

func (p *TotpProvider) Validate(code, secret string) bool {
	return totp.Validate(code, secret)
}
