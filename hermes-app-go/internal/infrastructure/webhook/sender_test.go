package webhook

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
)

type plainCipher struct{}

func (plainCipher) Encrypt(p, _ []byte) ([]byte, error) { return p, nil }
func (plainCipher) Decrypt(s, _ []byte) ([]byte, error) { return s, nil }

type fakeWebhooks struct {
	hooks map[domain.WebhookID]domain.Webhook
}

func (f fakeWebhooks) Insert(context.Context, domain.Webhook) error { return nil }
func (f fakeWebhooks) FindByID(_ context.Context, id domain.WebhookID) (domain.Webhook, error) {
	w, ok := f.hooks[id]
	if !ok {
		return domain.Webhook{}, application.ErrWebhookNotFound
	}
	return w, nil
}
func (f fakeWebhooks) ListByProject(context.Context, domain.ProjectID) ([]domain.Webhook, error) {
	return nil, nil
}
func (f fakeWebhooks) Disable(context.Context, domain.WebhookID) error { return nil }

type fakeDeliveries struct {
	mu        sync.Mutex
	due       []domain.WebhookDelivery
	delivered map[string]int
	failed    map[string]string
}

func (f *fakeDeliveries) Insert(context.Context, domain.WebhookDelivery) error { return nil }
func (f *fakeDeliveries) ClaimDue(context.Context, int, time.Duration) ([]domain.WebhookDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.due
	f.due = nil
	return out, nil
}
func (f *fakeDeliveries) MarkDelivered(_ context.Context, d domain.WebhookDelivery, code int) error {
	f.delivered[d.ID.String()] = code
	return nil
}
func (f *fakeDeliveries) MarkFailed(_ context.Context, d domain.WebhookDelivery, _ *int, msg string) error {
	f.failed[d.ID.String()] = msg
	return nil
}
func (f *fakeDeliveries) LatestByWebhook(context.Context, domain.WebhookID) (domain.WebhookDelivery, bool, error) {
	return domain.WebhookDelivery{}, false, nil
}

func setup(url string, allowPrivate bool) (*Sender, *fakeDeliveries, domain.WebhookDelivery) {
	hook := domain.Webhook{ID: domain.NewWebhookID(), URL: url, Events: []string{domain.EventTaskCompleted}, EncryptedSecret: []byte("whsec_test")}
	d := domain.NewWebhookDelivery(hook.ID, domain.EventTaskCompleted, []byte(`{"event":"task.completed"}`))
	deliveries := &fakeDeliveries{due: []domain.WebhookDelivery{d}, delivered: map[string]int{}, failed: map[string]string{}}
	s := NewSender(deliveries, fakeWebhooks{hooks: map[domain.WebhookID]domain.Webhook{hook.ID: hook}}, plainCipher{}, NewHTTPClient(allowPrivate), time.Second, 10)
	return s, deliveries, d
}

func TestSender_DeliversSignedPayload(t *testing.T) {
	var gotSig, gotTs string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Hermes-Signature")
		gotTs = r.Header.Get("X-Hermes-Timestamp")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s, deliveries, d := setup(srv.URL, true)
	s.Tick(context.Background())

	if deliveries.delivered[d.ID.String()] != http.StatusOK {
		t.Fatalf("expected delivered, failures: %v", deliveries.failed)
	}
	if want := application.SignWebhook([]byte("whsec_test"), gotTs, gotBody); gotSig != want {
		t.Fatalf("signature mismatch: got %s want %s", gotSig, want)
	}
}

func TestSender_Non2xxIsRetried(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer srv.Close()
	s, deliveries, d := setup(srv.URL, true)
	s.Tick(context.Background())
	if deliveries.failed[d.ID.String()] != "HTTP 502" {
		t.Fatalf("expected HTTP 502 failure, got %v", deliveries.failed)
	}
}

func TestSender_RefusesLoopbackTargetByDefault(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hit = true }))
	defer srv.Close()
	s, deliveries, d := setup(srv.URL, false)
	s.Tick(context.Background())
	if hit {
		t.Fatal("SSRF guard let a loopback request through")
	}
	if _, failed := deliveries.failed[d.ID.String()]; !failed {
		t.Fatal("delivery to a loopback target must be marked failed")
	}
}

func TestBlockedIP(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "172.18.0.5", "192.168.1.1", "169.254.169.254", "::1", "fd00::1"} {
		if !blockedIP(net.ParseIP(ip)) {
			t.Errorf("%s should be blocked", ip)
		}
	}
	if blockedIP(net.ParseIP("93.184.216.34")) {
		t.Error("a public address must not be blocked")
	}
}
