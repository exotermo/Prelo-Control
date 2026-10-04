package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/persistence/postgres"
)

func TestApiKeyRepository_HashLookupRevokeAndList(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	project := mustInsertProject(t, ctx, postgres.NewProjectRepository(pool), "P")
	repo := postgres.NewApiKeyRepository(pool)

	key, err := domain.NewApiKey(project, "app", []string{"tasks:create", "tasks:read"}, nil, "prl_live_abcd", []byte("hash-1"), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(ctx, key); err != nil {
		t.Fatalf("insert: %v", err)
	}
	found, err := repo.FindByHash(ctx, []byte("hash-1"))
	if err != nil || found.ID != key.ID || len(found.Scopes) != 2 {
		t.Fatalf("FindByHash: %+v %v", found, err)
	}
	if _, err := repo.FindByHash(ctx, []byte("nope")); !errors.Is(err, application.ErrApiKeyNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	if err := repo.TouchLastUsed(ctx, key.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := repo.Revoke(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Revoke(ctx, key.ID); !errors.Is(err, application.ErrApiKeyNotFound) {
		t.Fatalf("second revoke must be not-found, got %v", err)
	}
	list, err := repo.ListByProject(ctx, project)
	if err != nil || len(list) != 0 {
		t.Fatalf("revoked key must not be listed: %+v %v", list, err)
	}
}

func TestWebhookDeliveryRepository_ClaimRetryDeliver(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	project := mustInsertProject(t, ctx, postgres.NewProjectRepository(pool), "P")
	hooks := postgres.NewWebhookRepository(pool)
	deliveries := postgres.NewWebhookDeliveryRepository(pool)

	hook, err := domain.NewWebhook(project, "w", "https://example.com/hook", []string{domain.EventTaskCompleted}, false, "admin")
	if err != nil {
		t.Fatal(err)
	}
	hook.EncryptedSecret = []byte("enc")
	if err := hooks.Insert(ctx, hook); err != nil {
		t.Fatalf("insert webhook: %v", err)
	}
	listed, err := hooks.ListByProject(ctx, project)
	if err != nil || len(listed) != 1 || listed[0].Events[0] != domain.EventTaskCompleted {
		t.Fatalf("ListByProject: %+v %v", listed, err)
	}

	d := domain.NewWebhookDelivery(hook.ID, domain.EventTaskCompleted, []byte(`{"event":"task.completed","data":{"x":1}}`))
	if err := deliveries.Insert(ctx, d); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}

	claimed, err := deliveries.ClaimDue(ctx, 10, time.Minute)
	if err != nil || len(claimed) != 1 || claimed[0].Attempt != 1 || claimed[0].Status != domain.DeliverySending {
		t.Fatalf("first claim: %+v %v", claimed, err)
	}
	again, err := deliveries.ClaimDue(ctx, 10, time.Minute)
	if err != nil || len(again) != 0 {
		t.Fatalf("a leased row must not be claimed twice: %+v %v", again, err)
	}

	code := 500
	if err := deliveries.MarkFailed(ctx, claimed[0], &code, "HTTP 500"); err != nil {
		t.Fatal(err)
	}
	last, ok, err := deliveries.LatestByWebhook(ctx, hook.ID)
	if err != nil || !ok || last.Status != domain.DeliveryRetry || last.LastStatusCode == nil || *last.LastStatusCode != 500 {
		t.Fatalf("after failure: %+v ok=%v %v", last, ok, err)
	}
	if retry, _ := deliveries.ClaimDue(ctx, 10, time.Minute); len(retry) != 0 {
		t.Fatal("a RETRY row must wait for its backoff before being claimed again")
	}

	// Force the backoff to have elapsed, then deliver.
	if _, err := pool.Exec(ctx, `UPDATE webhook_deliveries SET available_at = now() - interval '1 second' WHERE id = $1`, d.ID); err != nil {
		t.Fatal(err)
	}
	retry, err := deliveries.ClaimDue(ctx, 10, time.Minute)
	if err != nil || len(retry) != 1 || retry[0].Attempt != 2 {
		t.Fatalf("retry claim: %+v %v", retry, err)
	}
	if err := deliveries.MarkDelivered(ctx, retry[0], 204); err != nil {
		t.Fatal(err)
	}
	last, _, _ = deliveries.LatestByWebhook(ctx, hook.ID)
	if last.Status != domain.DeliveryDelivered || last.DeliveredAt == nil {
		t.Fatalf("expected DELIVERED, got %+v", last)
	}
}

func TestWebhookDeliveryRepository_DeadAfterMaxAttempts(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	project := mustInsertProject(t, ctx, postgres.NewProjectRepository(pool), "P")
	hooks := postgres.NewWebhookRepository(pool)
	deliveries := postgres.NewWebhookDeliveryRepository(pool)

	hook, _ := domain.NewWebhook(project, "w", "https://example.com/hook", []string{domain.EventTaskFailed}, false, "admin")
	hook.EncryptedSecret = []byte("enc")
	if err := hooks.Insert(ctx, hook); err != nil {
		t.Fatal(err)
	}
	d := domain.NewWebhookDelivery(hook.ID, domain.EventTaskFailed, []byte(`{}`))
	if err := deliveries.Insert(ctx, d); err != nil {
		t.Fatal(err)
	}
	d.Attempt = domain.WebhookMaxAttempts
	if err := deliveries.MarkFailed(ctx, d, nil, "connection refused"); err != nil {
		t.Fatal(err)
	}
	last, _, _ := deliveries.LatestByWebhook(ctx, hook.ID)
	if last.Status != domain.DeliveryDead {
		t.Fatalf("expected DEAD after max attempts, got %s", last.Status)
	}
}
