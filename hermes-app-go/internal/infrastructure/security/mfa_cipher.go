package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// MfaCipher encrypts TOTP secrets at rest with AES-256-GCM — the raw secret is never stored,
// same design as messaging-core's DashboardMfaCipher. The AAD (the user id) binds a ciphertext
// to the row it belongs to: copying one user's encrypted secret into another user's row fails
// to decrypt instead of silently authenticating as the wrong account.
type MfaCipher struct {
	gcm cipher.AEAD
}

// NewMfaCipher takes a 32-byte key, Base64-encoded (same convention as messaging-core's
// MESSAGING_DASHBOARD_MFA_KEY: openssl rand -base64 32).
func NewMfaCipher(base64Key string) (*MfaCipher, error) {
	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("dashboard MFA key must be 32 random bytes encoded as Base64")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &MfaCipher{gcm: gcm}, nil
}

// Encrypt returns nonce||ciphertext, ready to store as-is.
func (c *MfaCipher) Encrypt(plaintext []byte, aad []byte) ([]byte, error) {
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return c.gcm.Seal(nonce, nonce, plaintext, aad), nil
}

func (c *MfaCipher) Decrypt(stored []byte, aad []byte) ([]byte, error) {
	size := c.gcm.NonceSize()
	if len(stored) < size {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := stored[:size], stored[size:]
	return c.gcm.Open(nil, nonce, ciphertext, aad)
}
