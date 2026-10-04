package domain

import (
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ApiKeyID struct{ Value uuid.UUID }

func NewApiKeyID() ApiKeyID { return ApiKeyID{Value: uuid.New()} }

func (id ApiKeyID) String() string { return id.Value.String() }

type WebhookID struct{ Value uuid.UUID }

func NewWebhookID() WebhookID { return WebhookID{Value: uuid.New()} }

func (id WebhookID) String() string { return id.Value.String() }

const MaxIntegrationNameLength = 120

// ApiKeyAllowedScopes is the whole set an external app may ever hold. approvals:decide is
// deliberately absent: deciding a REQUIRE_APPROVAL tool call is a human act (AGENTS.md), never
// something an integration can be granted.
var ApiKeyAllowedScopes = map[string]bool{
	"tasks:create": true, "tasks:read": true, "tasks:execute": true, "observability:read": true,
}

// ApiKey is a Prelo-issued credential for an external app (Fase I). Only KeyHash is stored —
// the raw "prl_live_…" value is shown once at creation and never again. It is always bound to one
// Project: everything it creates or reads is scoped there, with no X-Project-Id needed.
type ApiKey struct {
	ID            ApiKeyID
	ProjectID     ProjectID
	Name          string
	DisplayPrefix string
	KeyHash       []byte
	Scopes        []string
	ExpiresAt     *time.Time
	CreatedAt     time.Time
	CreatedBy     string
	LastUsedAt    *time.Time
	RevokedAt     *time.Time
}

func NewApiKey(projectID ProjectID, name string, scopes []string, expiresAt *time.Time, displayPrefix string, keyHash []byte, createdBy string) (ApiKey, error) {
	if isBlank(name) || len(name) > MaxIntegrationNameLength {
		return ApiKey{}, &ValidationError{Message: "name is required and must be at most 120 characters"}
	}
	if len(scopes) == 0 {
		return ApiKey{}, &ValidationError{Message: "at least one scope is required"}
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(scopes))
	for _, s := range scopes {
		if !ApiKeyAllowedScopes[s] {
			return ApiKey{}, &ValidationError{Message: "scope not allowed for an API key: " + s}
		}
		if !seen[s] {
			seen[s] = true
			clean = append(clean, s)
		}
	}
	return ApiKey{
		ID: NewApiKeyID(), ProjectID: projectID, Name: name, DisplayPrefix: displayPrefix, KeyHash: keyHash,
		Scopes: clean, ExpiresAt: expiresAt, CreatedAt: time.Now().UTC(), CreatedBy: createdBy,
	}, nil
}

// Usable reports whether the key may still authenticate a request at now.
func (k ApiKey) Usable(now time.Time) bool {
	if k.RevokedAt != nil {
		return false
	}
	return k.ExpiresAt == nil || now.Before(*k.ExpiresAt)
}

const (
	EventTaskCompleted   = "task.completed"
	EventTaskFailed      = "task.failed"
	EventApprovalPending = "approval.pending"
	EventServerOffline   = "server.offline"
	EventWebhookTest     = "webhook.test"
)

// WebhookSubscribableEvents is what a webhook can subscribe to; webhook.test is sent only on
// explicit request and reaches a webhook regardless of its subscriptions.
var WebhookSubscribableEvents = map[string]bool{
	EventTaskCompleted: true, EventTaskFailed: true, EventApprovalPending: true, EventServerOffline: true,
}

// Webhook is an outbound notification target for one Project (Fase I). EncryptedSecret signs
// every delivery (HMAC-SHA256); its plaintext is shown once at creation.
type Webhook struct {
	ID              WebhookID
	ProjectID       ProjectID
	Name            string
	URL             string
	Events          []string
	EncryptedSecret []byte
	CreatedAt       time.Time
	CreatedBy       string
	DisabledAt      *time.Time
}

func NewWebhook(projectID ProjectID, name, rawURL string, events []string, allowPlainHTTP bool, createdBy string) (Webhook, error) {
	if isBlank(name) || len(name) > MaxIntegrationNameLength {
		return Webhook{}, &ValidationError{Message: "name is required and must be at most 120 characters"}
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || len(rawURL) > 2000 {
		return Webhook{}, &ValidationError{Message: "url must be an absolute URL"}
	}
	if parsed.Scheme != "https" && !(allowPlainHTTP && parsed.Scheme == "http") {
		return Webhook{}, &ValidationError{Message: "url must use https"}
	}
	if parsed.User != nil {
		return Webhook{}, &ValidationError{Message: "url must not embed credentials"}
	}
	if len(events) == 0 {
		return Webhook{}, &ValidationError{Message: "at least one event is required"}
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(events))
	for _, e := range events {
		if !WebhookSubscribableEvents[e] {
			return Webhook{}, &ValidationError{Message: "unknown event: " + e}
		}
		if !seen[e] {
			seen[e] = true
			clean = append(clean, e)
		}
	}
	return Webhook{
		ID: NewWebhookID(), ProjectID: projectID, Name: name, URL: parsed.String(), Events: clean,
		CreatedAt: time.Now().UTC(), CreatedBy: createdBy,
	}, nil
}

func (w Webhook) Subscribes(event string) bool {
	if event == EventWebhookTest {
		return true
	}
	for _, e := range w.Events {
		if e == event {
			return true
		}
	}
	return false
}

type WebhookDeliveryStatus string

const (
	DeliveryPending   WebhookDeliveryStatus = "PENDING"
	DeliverySending   WebhookDeliveryStatus = "SENDING"
	DeliveryDelivered WebhookDeliveryStatus = "DELIVERED"
	DeliveryRetry     WebhookDeliveryStatus = "RETRY"
	DeliveryDead      WebhookDeliveryStatus = "DEAD"
)

// WebhookMaxAttempts bounds retries; after the last failure a delivery is DEAD.
const WebhookMaxAttempts = 8

// WebhookDelivery is one durable outbox row: written when an event fires, drained by the
// webhook sender loop with retries and exponential backoff.
type WebhookDelivery struct {
	ID             uuid.UUID
	WebhookID      WebhookID
	Event          string
	Payload        []byte
	Status         WebhookDeliveryStatus
	Attempt        int
	AvailableAt    time.Time
	LastStatusCode *int
	LastError      *string
	CreatedAt      time.Time
	DeliveredAt    *time.Time
}

func NewWebhookDelivery(webhookID WebhookID, event string, payload []byte) WebhookDelivery {
	now := time.Now().UTC()
	return WebhookDelivery{ID: uuid.New(), WebhookID: webhookID, Event: event, Payload: payload,
		Status: DeliveryPending, AvailableAt: now, CreatedAt: now}
}

// WebhookRetryDelay is the wait after a failed attempt number `attempt` (1-based):
// 10s, 20s, 40s … capped at one hour.
func WebhookRetryDelay(attempt int) time.Duration {
	d := 10 * time.Second
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= time.Hour {
			return time.Hour
		}
	}
	return d
}
