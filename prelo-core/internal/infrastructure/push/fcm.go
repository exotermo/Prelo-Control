// Package push is G11 (contratos): notifications to the Work Control app through Firebase Cloud
// Messaging (HTTP v1). Messages carry no content — only "something is waiting" plus opaque ids; the
// app fetches the details through the normal, authorized API.
package push

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	fcmScope       = "https://www.googleapis.com/auth/firebase.messaging"
	defaultFCMBase = "https://fcm.googleapis.com"
)

// ErrUnregistered means FCM no longer knows the token (app uninstalled or token rotated).
var ErrUnregistered = errors.New("fcm token unregistered")

// ServiceAccount is the subset of the Firebase service-account JSON the sender needs.
type ServiceAccount struct {
	ProjectID   string
	ClientEmail string
	TokenURI    string
	key         *rsa.PrivateKey
}

// LoadServiceAccount reads the service-account file. An empty path or empty file means push is
// off (nil, nil). Errors never include the key material.
func LoadServiceAccount(path string) (*ServiceAccount, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read service account: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	return ParseServiceAccount(raw)
}

func ParseServiceAccount(raw []byte) (*ServiceAccount, error) {
	var f struct {
		Type        string `json:"type"`
		ProjectID   string `json:"project_id"`
		PrivateKey  string `json:"private_key"`
		ClientEmail string `json:"client_email"`
		TokenURI    string `json:"token_uri"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, errors.New("service account is not valid JSON")
	}
	if f.Type != "service_account" || f.ProjectID == "" || f.ClientEmail == "" || f.PrivateKey == "" {
		return nil, errors.New("not a Firebase service-account file (type, project_id, client_email, private_key)")
	}
	block, _ := pem.Decode([]byte(f.PrivateKey))
	if block == nil {
		return nil, errors.New("service account private_key is not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("service account private_key is not PKCS#8")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("service account private_key is not RSA")
	}
	if f.TokenURI == "" {
		f.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return &ServiceAccount{ProjectID: f.ProjectID, ClientEmail: f.ClientEmail, TokenURI: f.TokenURI, key: key}, nil
}

// Message is what the Prelo sends: a fixed pt-BR text and opaque ids in data.
type Message struct {
	Token string
	Title string
	Body  string
	Tag   string            // same tag replaces the previous notification on the phone
	Data  map[string]string // ids only — never content
}

// FCMClient sends through FCM HTTP v1, authenticating with a short-lived OAuth token obtained by
// signing a JWT with the service account (no Google SDK needed).
type FCMClient struct {
	sa      *ServiceAccount
	http    *http.Client
	baseURL string
	now     func() time.Time

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

func NewFCMClient(sa *ServiceAccount) *FCMClient {
	return &FCMClient{sa: sa, http: &http.Client{Timeout: 10 * time.Second}, baseURL: defaultFCMBase, now: time.Now}
}

// ProjectID is the Firebase project the client sends through (shown in logs; not a secret).
func (c *FCMClient) ProjectID() string { return c.sa.ProjectID }

func (c *FCMClient) Send(ctx context.Context, m Message) error {
	token, err := c.oauthToken(ctx)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"message": map[string]any{
		"token":        m.Token,
		"notification": map[string]string{"title": m.Title, "body": m.Body},
		"data":         m.Data,
		"android": map[string]any{
			"priority":     "HIGH",
			"collapse_key": m.Tag,
			"notification": map[string]string{"channel_id": AndroidChannelID, "tag": m.Tag},
		},
	}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/projects/"+url.PathEscape(c.sa.ProjectID)+"/messages:send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("fcm send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	code := fcmErrorCode(resp.Body)
	if resp.StatusCode == http.StatusNotFound || code == "UNREGISTERED" {
		return ErrUnregistered
	}
	if resp.StatusCode == http.StatusUnauthorized {
		c.mu.Lock()
		c.accessToken = "" // refresh on the next send
		c.mu.Unlock()
	}
	return fmt.Errorf("fcm send: status %d %s", resp.StatusCode, code)
}

// fcmErrorCode extracts google.firebase.fcm.v1.FcmError.errorCode (or the generic status).
func fcmErrorCode(r io.Reader) string {
	var e struct {
		Error struct {
			Status  string `json:"status"`
			Details []struct {
				ErrorCode string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(r, 64<<10)).Decode(&e) != nil {
		return ""
	}
	for _, d := range e.Error.Details {
		if d.ErrorCode != "" {
			return d.ErrorCode
		}
	}
	return e.Error.Status
}

func (c *FCMClient) oauthToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.accessToken != "" && now.Before(c.expiresAt.Add(-5*time.Minute)) {
		return c.accessToken, nil
	}
	assertion, err := c.signedAssertion(now)
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sa.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("fcm oauth: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out) != nil || out.AccessToken == "" {
		return "", fmt.Errorf("fcm oauth: status %d", resp.StatusCode)
	}
	c.accessToken = out.AccessToken
	c.expiresAt = now.Add(time.Duration(out.ExpiresIn) * time.Second)
	return c.accessToken, nil
}

func (c *FCMClient) signedAssertion(now time.Time) (string, error) {
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{
		"iss": c.sa.ClientEmail, "scope": fcmScope, "aud": c.sa.TokenURI,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	signing := header + "." + enc.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.sa.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", errors.New("fcm oauth: sign assertion")
	}
	return signing + "." + enc.EncodeToString(sig), nil
}
