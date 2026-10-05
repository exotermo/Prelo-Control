package push

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/infrastructure/realtime"
)

func testServiceAccount(t *testing.T, tokenURI string) *ServiceAccount {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	raw, _ := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "prelo-test", "client_email": "push@prelo-test.iam.gserviceaccount.com",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), "token_uri": tokenURI,
	})
	sa, err := ParseServiceAccount(raw)
	if err != nil {
		t.Fatal(err)
	}
	return sa
}

func TestLoadServiceAccountOffWhenUnset(t *testing.T) {
	if sa, err := LoadServiceAccount(""); sa != nil || err != nil {
		t.Fatalf("empty path must mean off, got %v %v", sa, err)
	}
	if sa, err := LoadServiceAccount("/dev/null"); sa != nil || err != nil {
		t.Fatalf("empty file must mean off, got %v %v", sa, err)
	}
	if _, err := ParseServiceAccount([]byte(`{"type":"authorized_user"}`)); err == nil {
		t.Fatal("non service-account file must be rejected")
	}
}

func TestFCMClientSendsWithOAuthAndDetectsUnregistered(t *testing.T) {
	var mu sync.Mutex
	oauthCalls := 0
	var sent []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || strings.Count(r.Form.Get("assertion"), ".") != 2 {
				http.Error(w, "bad", 400)
				return
			}
			mu.Lock()
			oauthCalls++
			mu.Unlock()
			_, _ = w.Write([]byte(`{"access_token":"ya29.test","expires_in":3600}`))
		case r.URL.Path == "/v1/projects/prelo-test/messages:send":
			if r.Header.Get("Authorization") != "Bearer ya29.test" {
				http.Error(w, "unauth", 401)
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			msg := body["message"].(map[string]any)
			if msg["token"] == "gone" {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`))
				return
			}
			mu.Lock()
			sent = append(sent, msg)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"name":"projects/prelo-test/messages/1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewFCMClient(testServiceAccount(t, srv.URL+"/token"))
	c.baseURL = srv.URL
	m := Message{Token: "abc", Title: "Prelo Control", Body: "x", Tag: "approval", Data: map[string]string{"kind": "approval"}}
	if err := c.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	m.Token = "gone"
	if err := c.Send(context.Background(), m); err != ErrUnregistered {
		t.Fatalf("want ErrUnregistered, got %v", err)
	}
	if oauthCalls != 1 {
		t.Fatalf("oauth token must be cached, got %d calls", oauthCalls)
	}
	android := sent[0]["android"].(map[string]any)
	if android["notification"].(map[string]any)["channel_id"] != AndroidChannelID {
		t.Fatalf("missing channel: %v", android)
	}
}

func TestMessageForCarriesOnlyIDs(t *testing.T) {
	p := uuid.New()
	ar := "ar-1"
	m, ok := MessageFor(realtime.Event{Kind: "approval", ID: "ap-1", ProjectID: &p, Status: "PENDING", ActionRequestID: &ar})
	if !ok || m.Data["id"] != "ap-1" || m.Data["projectId"] != p.String() || m.Data["actionRequestId"] != "ar-1" {
		t.Fatalf("unexpected %+v", m)
	}
	if _, ok := MessageFor(realtime.Event{Kind: "approval", ID: "ap-1", Status: "APPROVED"}); ok {
		t.Fatal("decided approval must not push")
	}
	if _, ok := MessageFor(realtime.Event{Kind: "action", ID: "a", Status: "RUNNING"}); ok {
		t.Fatal("RUNNING must not push")
	}
	if m, ok := MessageFor(realtime.Event{Kind: "action", ID: "a", Status: "FAILED"}); !ok || m.Body != "Um deploy falhou." {
		t.Fatalf("unexpected %+v", m)
	}
	if _, ok := MessageFor(realtime.Event{Kind: "task", ID: "t", Status: "RUNNING"}); ok {
		t.Fatal("tasks must not push")
	}
}

type fakeTargets struct {
	mu        sync.Mutex
	tokens    []string
	forgotten []string
}

func (f *fakeTargets) PushTargets(context.Context, *uuid.UUID, time.Time) ([]string, error) {
	return f.tokens, nil
}
func (f *fakeTargets) ForgetPushToken(_ context.Context, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forgotten = append(f.forgotten, token)
	return nil
}

type fakeSender struct {
	mu   sync.Mutex
	sent []Message
}

func (f *fakeSender) Send(_ context.Context, m Message) error {
	if m.Token == "gone" {
		return ErrUnregistered
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return nil
}

func TestNotifierDeliversAndForgetsDeadTokens(t *testing.T) {
	hub := realtime.NewHub()
	targets := &fakeTargets{tokens: []string{"t1", "gone", "t2"}}
	sender := &fakeSender{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := NewNotifier(hub, targets, sender)
	go n.Run(ctx)
	time.Sleep(20 * time.Millisecond) // let Run subscribe
	hub.Publish(realtime.Event{Kind: "task", ID: "t", Status: "RUNNING"})
	hub.Publish(realtime.Event{Kind: "approval", ID: "ap", Status: "PENDING"})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sender.mu.Lock()
		done := len(sender.sent) == 2
		sender.mu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.sent) != 2 || sender.sent[0].Data["kind"] != "approval" {
		t.Fatalf("unexpected sends %+v", sender.sent)
	}
	targets.mu.Lock()
	defer targets.mu.Unlock()
	if len(targets.forgotten) != 1 || targets.forgotten[0] != "gone" {
		t.Fatalf("dead token not forgotten: %v", targets.forgotten)
	}
}
