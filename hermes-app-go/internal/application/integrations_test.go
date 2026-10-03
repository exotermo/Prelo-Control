package application

import (
	"context"
	"testing"
	"time"

	"github.com/exotermo/hermes-app-go/internal/domain"
)

type memWebhooks struct{ list []domain.Webhook }

func (m *memWebhooks) Insert(_ context.Context, w domain.Webhook) error {
	m.list = append(m.list, w)
	return nil
}
func (m *memWebhooks) FindByID(_ context.Context, id domain.WebhookID) (domain.Webhook, error) {
	for _, w := range m.list {
		if w.ID == id {
			return w, nil
		}
	}
	return domain.Webhook{}, ErrWebhookNotFound
}
func (m *memWebhooks) ListByProject(_ context.Context, p domain.ProjectID) ([]domain.Webhook, error) {
	var out []domain.Webhook
	for _, w := range m.list {
		if w.ProjectID == p && w.DisabledAt == nil {
			out = append(out, w)
		}
	}
	return out, nil
}
func (m *memWebhooks) Disable(context.Context, domain.WebhookID) error { return nil }

type memDeliveries struct{ inserted []domain.WebhookDelivery }

func (m *memDeliveries) Insert(_ context.Context, d domain.WebhookDelivery) error {
	m.inserted = append(m.inserted, d)
	return nil
}
func (m *memDeliveries) ClaimDue(context.Context, int, time.Duration) ([]domain.WebhookDelivery, error) {
	return nil, nil
}
func (m *memDeliveries) MarkDelivered(context.Context, domain.WebhookDelivery, int) error { return nil }
func (m *memDeliveries) MarkFailed(context.Context, domain.WebhookDelivery, *int, string) error {
	return nil
}
func (m *memDeliveries) LatestByWebhook(context.Context, domain.WebhookID) (domain.WebhookDelivery, bool, error) {
	return domain.WebhookDelivery{}, false, nil
}

func TestWebhookDispatcher_OnlySubscribedWebhooksOfTheProject(t *testing.T) {
	project := domain.NewProjectID()
	other := domain.NewProjectID()
	subscribed := domain.Webhook{ID: domain.NewWebhookID(), ProjectID: project, Events: []string{domain.EventTaskCompleted}}
	notSubscribed := domain.Webhook{ID: domain.NewWebhookID(), ProjectID: project, Events: []string{domain.EventTaskFailed}}
	otherProject := domain.Webhook{ID: domain.NewWebhookID(), ProjectID: other, Events: []string{domain.EventTaskCompleted}}
	hooks := &memWebhooks{list: []domain.Webhook{subscribed, notSubscribed, otherProject}}
	deliveries := &memDeliveries{}

	d := NewWebhookDispatcher(hooks, deliveries)
	d.Publish(context.Background(), &project, domain.EventTaskCompleted, map[string]any{"taskId": "t"})
	d.Publish(context.Background(), nil, domain.EventTaskCompleted, map[string]any{"taskId": "t"})

	if len(deliveries.inserted) != 1 || deliveries.inserted[0].WebhookID != subscribed.ID {
		t.Fatalf("expected exactly one delivery to the subscribed webhook, got %+v", deliveries.inserted)
	}
}

func TestSignWebhook_IsDeterministicAndKeyed(t *testing.T) {
	a := SignWebhook([]byte("s1"), "100", []byte("body"))
	if a != SignWebhook([]byte("s1"), "100", []byte("body")) {
		t.Fatal("signature must be deterministic")
	}
	if a == SignWebhook([]byte("s2"), "100", []byte("body")) || a == SignWebhook([]byte("s1"), "101", []byte("body")) {
		t.Fatal("signature must depend on secret and timestamp")
	}
}

type fakeApiKeys struct{ keys []domain.ApiKey }

func (f *fakeApiKeys) Insert(_ context.Context, k domain.ApiKey) error {
	f.keys = append(f.keys, k)
	return nil
}
func (f *fakeApiKeys) FindByID(_ context.Context, id domain.ApiKeyID) (domain.ApiKey, error) {
	for _, k := range f.keys {
		if k.ID == id {
			return k, nil
		}
	}
	return domain.ApiKey{}, ErrApiKeyNotFound
}
func (f *fakeApiKeys) FindByHash(_ context.Context, h []byte) (domain.ApiKey, error) {
	for _, k := range f.keys {
		if string(k.KeyHash) == string(h) {
			return k, nil
		}
	}
	return domain.ApiKey{}, ErrApiKeyNotFound
}
func (f *fakeApiKeys) ListByProject(context.Context, domain.ProjectID) ([]domain.ApiKey, error) {
	return f.keys, nil
}
func (f *fakeApiKeys) Revoke(_ context.Context, id domain.ApiKeyID) error {
	for i := range f.keys {
		if f.keys[i].ID == id {
			now := time.Now()
			f.keys[i].RevokedAt = &now
		}
	}
	return nil
}
func (f *fakeApiKeys) TouchLastUsed(context.Context, domain.ApiKeyID, time.Time) error { return nil }

func TestIntegrationService_ApiKeyLifecycle(t *testing.T) {
	keys := &fakeApiKeys{}
	svc := NewIntegrationService(keys, &memWebhooks{}, &memDeliveries{}, nil, false)
	project := domain.NewProjectID()

	key, raw, err := svc.CreateApiKey(context.Background(), project, "app", []string{"tasks:create"}, 90, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 40 || raw[:len(ApiKeyPrefix)] != ApiKeyPrefix || string(key.KeyHash) == raw {
		t.Fatalf("raw key must be prefixed and only its hash stored: %q", raw)
	}
	if _, err := svc.AuthenticateApiKey(context.Background(), raw); err != nil {
		t.Fatalf("fresh key must authenticate: %v", err)
	}
	if err := svc.RevokeApiKey(context.Background(), domain.NewProjectID(), key.ID); err != ErrApiKeyNotFound {
		t.Fatalf("revoking from another project must look like not-found, got %v", err)
	}
	if err := svc.RevokeApiKey(context.Background(), project, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateApiKey(context.Background(), raw); err != ErrApiKeyUnusable {
		t.Fatalf("revoked key must not authenticate, got %v", err)
	}
}
