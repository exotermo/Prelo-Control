package webhook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
)

const (
	requestTimeout = 10 * time.Second
	claimLease     = 60 * time.Second
)

var errPrivateTarget = errors.New("webhook target resolves to a private, loopback or link-local address")

// blockedIP is the SSRF guard: a webhook URL is user-supplied, and hermes-go sits on networks
// that reach Postgres, Redis, the LLM gateway and the bridge — none of those may ever be a
// webhook target.
func blockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast()
}

// NewHTTPClient returns the client used for deliveries. The check runs in the dialer's Control
// hook, i.e. on the address actually being connected to after DNS resolution — so a public
// hostname that resolves to an internal IP is refused too. Redirects are never followed.
func NewHTTPClient(allowPrivateTargets bool) *http.Client {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			if allowPrivateTargets {
				return nil
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || blockedIP(ip) {
				return errPrivateTarget
			}
			return nil
		},
	}
	transport := &http.Transport{DialContext: dialer.DialContext, Proxy: nil, TLSHandshakeTimeout: 5 * time.Second}
	return &http.Client{
		Transport:     transport,
		Timeout:       requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Sender drains the webhook outbox: same ticker shape as worker.Sweeper.
type Sender struct {
	deliveries application.WebhookDeliveryRepository
	webhooks   application.WebhookRepository
	cipher     application.SecretCipher
	client     *http.Client
	interval   time.Duration
	batchSize  int
}

func NewSender(deliveries application.WebhookDeliveryRepository, webhooks application.WebhookRepository, cipher application.SecretCipher, client *http.Client, interval time.Duration, batchSize int) *Sender {
	return &Sender{deliveries: deliveries, webhooks: webhooks, cipher: cipher, client: client, interval: interval, batchSize: batchSize}
}

func (s *Sender) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Tick(ctx)
		}
	}
}

func (s *Sender) Tick(ctx context.Context) {
	due, err := s.deliveries.ClaimDue(ctx, s.batchSize, claimLease)
	if err != nil {
		log.Printf("webhooks: claim due deliveries failed: %v", err)
		return
	}
	for _, d := range due {
		s.deliver(ctx, d)
	}
}

func (s *Sender) deliver(ctx context.Context, d domain.WebhookDelivery) {
	webhook, err := s.webhooks.FindByID(ctx, d.WebhookID)
	if err != nil || webhook.DisabledAt != nil {
		s.fail(ctx, d, nil, "webhook removed")
		return
	}
	secret, err := application.DecryptWebhookSecret(s.cipher, webhook)
	if err != nil {
		s.fail(ctx, d, nil, "could not decrypt signing secret")
		return
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook.URL, bytes.NewReader(d.Payload))
	if err != nil {
		s.fail(ctx, d, nil, "invalid url")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Hermes-Webhooks/1")
	req.Header.Set("X-Hermes-Event", d.Event)
	req.Header.Set("X-Hermes-Delivery", d.ID.String())
	req.Header.Set("X-Hermes-Timestamp", timestamp)
	req.Header.Set("X-Hermes-Signature", application.SignWebhook(secret, timestamp, d.Payload))

	resp, err := s.client.Do(req)
	if err != nil {
		s.fail(ctx, d, nil, err.Error())
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	_ = resp.Body.Close()

	code := resp.StatusCode
	if code >= 200 && code < 300 {
		if err := s.deliveries.MarkDelivered(ctx, d, code); err != nil {
			log.Printf("webhooks: mark delivered %s failed: %v", d.ID, err)
		}
		return
	}
	s.fail(ctx, d, &code, fmt.Sprintf("HTTP %d", code))
}

func (s *Sender) fail(ctx context.Context, d domain.WebhookDelivery, code *int, message string) {
	if err := s.deliveries.MarkFailed(ctx, d, code, message); err != nil {
		log.Printf("webhooks: mark failed %s failed: %v", d.ID, err)
		return
	}
	log.Printf("webhooks: delivery %s (%s) attempt %d failed: %s", d.ID, d.Event, d.Attempt, message)
}
