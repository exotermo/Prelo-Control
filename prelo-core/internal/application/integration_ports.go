package application

import (
	"context"
	"errors"
	"time"

	"github.com/exotermo/prelo-core/internal/domain"
)

var ErrApiKeyNotFound = errors.New("api key not found")
var ErrApiKeyUnusable = errors.New("api key revoked or expired")
var ErrWebhookNotFound = errors.New("webhook not found")

type ApiKeyRepository interface {
	Insert(ctx context.Context, key domain.ApiKey) error
	FindByID(ctx context.Context, id domain.ApiKeyID) (domain.ApiKey, error)
	FindByHash(ctx context.Context, hash []byte) (domain.ApiKey, error)
	// ListByProject excludes revoked keys, newest first.
	ListByProject(ctx context.Context, projectID domain.ProjectID) ([]domain.ApiKey, error)
	Revoke(ctx context.Context, id domain.ApiKeyID) error
	TouchLastUsed(ctx context.Context, id domain.ApiKeyID, at time.Time) error
}

type WebhookRepository interface {
	Insert(ctx context.Context, webhook domain.Webhook) error
	FindByID(ctx context.Context, id domain.WebhookID) (domain.Webhook, error)
	// ListByProject excludes disabled webhooks, newest first.
	ListByProject(ctx context.Context, projectID domain.ProjectID) ([]domain.Webhook, error)
	Disable(ctx context.Context, id domain.WebhookID) error
}

// WebhookDeliveryRepository is the outbox. ClaimDue atomically moves due rows to SENDING with a
// lease (FOR UPDATE SKIP LOCKED) and increments Attempt, so a crashed sender's rows come back
// once the lease expires; MarkFailed decides RETRY vs. DEAD from the attempt it is given.
type WebhookDeliveryRepository interface {
	Insert(ctx context.Context, delivery domain.WebhookDelivery) error
	ClaimDue(ctx context.Context, limit int, lease time.Duration) ([]domain.WebhookDelivery, error)
	MarkDelivered(ctx context.Context, delivery domain.WebhookDelivery, statusCode int) error
	MarkFailed(ctx context.Context, delivery domain.WebhookDelivery, statusCode *int, message string) error
	LatestByWebhook(ctx context.Context, webhookID domain.WebhookID) (domain.WebhookDelivery, bool, error)
}

// SecretCipher is the narrow port over security.MfaCipher used for webhook signing secrets — a
// separate instance keyed by PRELO_INTEGRATIONS_KEY, never the TOTP or SSH-credential key.
type SecretCipher interface {
	Encrypt(plaintext []byte, aad []byte) ([]byte, error)
	Decrypt(stored []byte, aad []byte) ([]byte, error)
}

// EventPublisher fans one domain event out to whatever is subscribed (today: the project's
// webhooks). It never returns an error — publishing is best-effort from the caller's point of
// view and must never fail the task/approval/health-check that triggered it.
type EventPublisher interface {
	Publish(ctx context.Context, projectID *domain.ProjectID, event string, data map[string]any)
}

type NoopEventPublisher struct{}

func (NoopEventPublisher) Publish(context.Context, *domain.ProjectID, string, map[string]any) {}
