package application

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/exotermo/hermes-app-go/internal/domain"
)

const ApiKeyPrefix = "hk_live_"

// IntegrationService is Fase I's whole application surface: issuing/revoking per-project API
// keys, authenticating a raw key, and registering/removing/testing webhooks. Every operation that
// takes an id also takes the caller's project and refuses (as not-found) anything outside it.
type IntegrationService struct {
	apiKeys        ApiKeyRepository
	webhooks       WebhookRepository
	deliveries     WebhookDeliveryRepository
	cipher         SecretCipher
	allowPlainHTTP bool
}

func NewIntegrationService(apiKeys ApiKeyRepository, webhooks WebhookRepository, deliveries WebhookDeliveryRepository, cipher SecretCipher, allowPlainHTTP bool) *IntegrationService {
	return &IntegrationService{apiKeys: apiKeys, webhooks: webhooks, deliveries: deliveries, cipher: cipher, allowPlainHTTP: allowPlainHTTP}
}

func randomToken(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

// CreateApiKey returns the stored key and its raw value — the only time the raw value exists
// outside the caller's hands.
func (s *IntegrationService) CreateApiKey(ctx context.Context, projectID domain.ProjectID, name string, scopes []string, expiresInDays int, createdBy string) (domain.ApiKey, string, error) {
	if expiresInDays < 0 || expiresInDays > 3650 {
		return domain.ApiKey{}, "", &domain.ValidationError{Message: "expiresInDays must be between 0 (never) and 3650"}
	}
	raw := ApiKeyPrefix + randomToken(32)
	var expiresAt *time.Time
	if expiresInDays > 0 {
		t := time.Now().UTC().Add(time.Duration(expiresInDays) * 24 * time.Hour)
		expiresAt = &t
	}
	key, err := domain.NewApiKey(projectID, name, scopes, expiresAt, raw[:len(ApiKeyPrefix)+4], hashToken(raw), createdBy)
	if err != nil {
		return domain.ApiKey{}, "", err
	}
	if err := s.apiKeys.Insert(ctx, key); err != nil {
		return domain.ApiKey{}, "", err
	}
	return key, raw, nil
}

// AuthenticateApiKey resolves a raw "hk_live_…" bearer to its key, or ErrApiKeyUnusable /
// ErrApiKeyNotFound. Recording last use is best-effort and never fails the request.
func (s *IntegrationService) AuthenticateApiKey(ctx context.Context, raw string) (domain.ApiKey, error) {
	key, err := s.apiKeys.FindByHash(ctx, hashToken(raw))
	if err != nil {
		return domain.ApiKey{}, err
	}
	now := time.Now().UTC()
	if !key.Usable(now) {
		return domain.ApiKey{}, ErrApiKeyUnusable
	}
	if err := s.apiKeys.TouchLastUsed(ctx, key.ID, now); err != nil {
		log.Printf("integrations: could not record api key use: %v", err)
	}
	return key, nil
}

func (s *IntegrationService) RevokeApiKey(ctx context.Context, projectID domain.ProjectID, id domain.ApiKeyID) error {
	key, err := s.apiKeys.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if key.ProjectID != projectID || key.RevokedAt != nil {
		return ErrApiKeyNotFound
	}
	return s.apiKeys.Revoke(ctx, id)
}

// CreateWebhook returns the stored webhook and its plaintext signing secret (shown once).
func (s *IntegrationService) CreateWebhook(ctx context.Context, projectID domain.ProjectID, name, url string, events []string, createdBy string) (domain.Webhook, string, error) {
	webhook, err := domain.NewWebhook(projectID, name, url, events, s.allowPlainHTTP, createdBy)
	if err != nil {
		return domain.Webhook{}, "", err
	}
	secret := "whsec_" + randomToken(24)
	encrypted, err := s.cipher.Encrypt([]byte(secret), []byte(webhook.ID.String()))
	if err != nil {
		return domain.Webhook{}, "", err
	}
	webhook.EncryptedSecret = encrypted
	if err := s.webhooks.Insert(ctx, webhook); err != nil {
		return domain.Webhook{}, "", err
	}
	return webhook, secret, nil
}

func (s *IntegrationService) findWebhook(ctx context.Context, projectID domain.ProjectID, id domain.WebhookID) (domain.Webhook, error) {
	webhook, err := s.webhooks.FindByID(ctx, id)
	if err != nil {
		return domain.Webhook{}, err
	}
	if webhook.ProjectID != projectID || webhook.DisabledAt != nil {
		return domain.Webhook{}, ErrWebhookNotFound
	}
	return webhook, nil
}

func (s *IntegrationService) DeleteWebhook(ctx context.Context, projectID domain.ProjectID, id domain.WebhookID) error {
	if _, err := s.findWebhook(ctx, projectID, id); err != nil {
		return err
	}
	return s.webhooks.Disable(ctx, id)
}

func (s *IntegrationService) SendTest(ctx context.Context, projectID domain.ProjectID, id domain.WebhookID) error {
	webhook, err := s.findWebhook(ctx, projectID, id)
	if err != nil {
		return err
	}
	payload, err := webhookPayload(&projectID, domain.EventWebhookTest, map[string]any{"message": "Evento de teste enviado pelo Hermes"})
	if err != nil {
		return err
	}
	return s.deliveries.Insert(ctx, domain.NewWebhookDelivery(webhook.ID, domain.EventWebhookTest, payload))
}

type WebhookWithLastDelivery struct {
	Webhook      domain.Webhook
	LastDelivery *domain.WebhookDelivery
}

func (s *IntegrationService) List(ctx context.Context, projectID domain.ProjectID) ([]domain.ApiKey, []WebhookWithLastDelivery, error) {
	keys, err := s.apiKeys.ListByProject(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	webhooks, err := s.webhooks.ListByProject(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	out := make([]WebhookWithLastDelivery, 0, len(webhooks))
	for _, w := range webhooks {
		entry := WebhookWithLastDelivery{Webhook: w}
		if last, ok, err := s.deliveries.LatestByWebhook(ctx, w.ID); err == nil && ok {
			entry.LastDelivery = &last
		}
		out = append(out, entry)
	}
	return keys, out, nil
}

// WebhookDispatcher implements EventPublisher by writing one outbox row per subscribed,
// active webhook of the event's project. Events outside any project (the pre-Fase-W bucket)
// reach no webhook — webhooks only ever belong to a project.
type WebhookDispatcher struct {
	webhooks   WebhookRepository
	deliveries WebhookDeliveryRepository
}

func NewWebhookDispatcher(webhooks WebhookRepository, deliveries WebhookDeliveryRepository) *WebhookDispatcher {
	return &WebhookDispatcher{webhooks: webhooks, deliveries: deliveries}
}

func (d *WebhookDispatcher) Publish(ctx context.Context, projectID *domain.ProjectID, event string, data map[string]any) {
	if projectID == nil {
		return
	}
	webhooks, err := d.webhooks.ListByProject(ctx, *projectID)
	if err != nil {
		log.Printf("webhooks: listing webhooks for project %s failed: %v", projectID, err)
		return
	}
	var payload []byte
	for _, w := range webhooks {
		if !w.Subscribes(event) {
			continue
		}
		if payload == nil {
			if payload, err = webhookPayload(projectID, event, data); err != nil {
				log.Printf("webhooks: encoding %s payload failed: %v", event, err)
				return
			}
		}
		if err := d.deliveries.Insert(ctx, domain.NewWebhookDelivery(w.ID, event, payload)); err != nil {
			log.Printf("webhooks: enqueue %s for webhook %s failed: %v", event, w.ID, err)
		}
	}
}

func webhookPayload(projectID *domain.ProjectID, event string, data map[string]any) ([]byte, error) {
	body := map[string]any{"event": event, "occurredAt": time.Now().UTC().Format(time.RFC3339), "data": data}
	if projectID != nil {
		body["projectId"] = projectID.String()
	}
	return json.Marshal(body)
}

// SignWebhook is the value of X-Hermes-Signature: "sha256=" + hex(HMAC-SHA256(secret,
// timestamp + "." + body)). Binding the timestamp lets a receiver reject replays.
func SignWebhook(secret []byte, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// DecryptWebhookSecret is what the sender uses to recover a webhook's signing secret.
func DecryptWebhookSecret(cipher SecretCipher, webhook domain.Webhook) ([]byte, error) {
	if cipher == nil {
		return nil, errors.New("integrations cipher not configured")
	}
	return cipher.Decrypt(webhook.EncryptedSecret, []byte(webhook.ID.String()))
}
