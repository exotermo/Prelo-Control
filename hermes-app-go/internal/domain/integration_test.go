package domain

import (
	"testing"
	"time"
)

func TestNewApiKey_RejectsApprovalScope(t *testing.T) {
	_, err := NewApiKey(NewProjectID(), "app", []string{"tasks:create", "approvals:decide"}, nil, "hk_live_abcd", []byte("h"), "admin")
	if err == nil {
		t.Fatal("expected approvals:decide to be refused for an API key")
	}
}

func TestNewApiKey_DedupesScopes(t *testing.T) {
	key, err := NewApiKey(NewProjectID(), "app", []string{"tasks:read", "tasks:read"}, nil, "hk_live_abcd", []byte("h"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(key.Scopes) != 1 {
		t.Fatalf("expected deduped scopes, got %v", key.Scopes)
	}
}

func TestApiKey_Usable(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)
	if (ApiKey{ExpiresAt: &past}).Usable(now) {
		t.Fatal("expired key must not be usable")
	}
	if (ApiKey{RevokedAt: &past}).Usable(now) {
		t.Fatal("revoked key must not be usable")
	}
	if !(ApiKey{}).Usable(now) {
		t.Fatal("key without expiry must be usable")
	}
}

func TestNewWebhook_URLRules(t *testing.T) {
	events := []string{EventTaskCompleted}
	if _, err := NewWebhook(NewProjectID(), "w", "http://example.com/x", events, false, "a"); err == nil {
		t.Fatal("plain http must be refused without the dev flag")
	}
	if _, err := NewWebhook(NewProjectID(), "w", "http://example.com/x", events, true, "a"); err != nil {
		t.Fatalf("plain http allowed with dev flag: %v", err)
	}
	if _, err := NewWebhook(NewProjectID(), "w", "https://user:pw@example.com/x", events, false, "a"); err == nil {
		t.Fatal("embedded credentials must be refused")
	}
	if _, err := NewWebhook(NewProjectID(), "w", "https://example.com/x", []string{"webhook.test"}, false, "a"); err == nil {
		t.Fatal("webhook.test is not subscribable")
	}
}

func TestWebhookRetryDelay(t *testing.T) {
	if WebhookRetryDelay(1) != 10*time.Second || WebhookRetryDelay(3) != 40*time.Second {
		t.Fatalf("unexpected backoff: %v %v", WebhookRetryDelay(1), WebhookRetryDelay(3))
	}
	if WebhookRetryDelay(20) != time.Hour {
		t.Fatalf("backoff must cap at one hour, got %v", WebhookRetryDelay(20))
	}
}
