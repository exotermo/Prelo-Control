package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// --- api keys ---

type ApiKeyRepository struct{ pool *pgxpool.Pool }

func NewApiKeyRepository(pool *pgxpool.Pool) *ApiKeyRepository { return &ApiKeyRepository{pool: pool} }

const apiKeySelect = `SELECT id, project_id, name, display_prefix, key_hash, scopes, expires_at, created_at,
	created_by, last_used_at, revoked_at FROM api_keys`

func (r *ApiKeyRepository) Insert(ctx context.Context, k domain.ApiKey) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO api_keys (id, project_id, name, display_prefix, key_hash, scopes, expires_at, created_at, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		k.ID.Value, k.ProjectID.Value, k.Name, k.DisplayPrefix, k.KeyHash, k.Scopes, k.ExpiresAt, k.CreatedAt, nullString(k.CreatedBy))
	return err
}

func (r *ApiKeyRepository) FindByID(ctx context.Context, id domain.ApiKeyID) (domain.ApiKey, error) {
	return scanApiKey(r.pool.QueryRow(ctx, apiKeySelect+" WHERE id = $1", id.Value))
}

func (r *ApiKeyRepository) FindByHash(ctx context.Context, hash []byte) (domain.ApiKey, error) {
	return scanApiKey(r.pool.QueryRow(ctx, apiKeySelect+" WHERE key_hash = $1", hash))
}

func (r *ApiKeyRepository) ListByProject(ctx context.Context, projectID domain.ProjectID) ([]domain.ApiKey, error) {
	rows, err := r.pool.Query(ctx, apiKeySelect+" WHERE project_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC", projectID.Value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []domain.ApiKey
	for rows.Next() {
		k, err := scanApiKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (r *ApiKeyRepository) Revoke(ctx context.Context, id domain.ApiKeyID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE api_keys SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id.Value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return application.ErrApiKeyNotFound
	}
	return nil
}

func (r *ApiKeyRepository) TouchLastUsed(ctx context.Context, id domain.ApiKeyID, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = $2 WHERE id = $1`, id.Value, at)
	return err
}

func scanApiKey(row pgx.Row) (domain.ApiKey, error) {
	var k domain.ApiKey
	var createdBy *string
	if err := row.Scan(&k.ID.Value, &k.ProjectID.Value, &k.Name, &k.DisplayPrefix, &k.KeyHash, &k.Scopes, &k.ExpiresAt,
		&k.CreatedAt, &createdBy, &k.LastUsedAt, &k.RevokedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ApiKey{}, application.ErrApiKeyNotFound
		}
		return domain.ApiKey{}, err
	}
	if createdBy != nil {
		k.CreatedBy = *createdBy
	}
	return k, nil
}

// --- webhooks ---

type WebhookRepository struct{ pool *pgxpool.Pool }

func NewWebhookRepository(pool *pgxpool.Pool) *WebhookRepository {
	return &WebhookRepository{pool: pool}
}

const webhookSelect = `SELECT id, project_id, name, url, events, encrypted_secret, created_at, created_by, disabled_at FROM webhooks`

func (r *WebhookRepository) Insert(ctx context.Context, w domain.Webhook) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO webhooks (id, project_id, name, url, events, encrypted_secret, created_at, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		w.ID.Value, w.ProjectID.Value, w.Name, w.URL, w.Events, w.EncryptedSecret, w.CreatedAt, nullString(w.CreatedBy))
	return err
}

func (r *WebhookRepository) FindByID(ctx context.Context, id domain.WebhookID) (domain.Webhook, error) {
	return scanWebhook(r.pool.QueryRow(ctx, webhookSelect+" WHERE id = $1", id.Value))
}

func (r *WebhookRepository) ListByProject(ctx context.Context, projectID domain.ProjectID) ([]domain.Webhook, error) {
	rows, err := r.pool.Query(ctx, webhookSelect+" WHERE project_id = $1 AND disabled_at IS NULL ORDER BY created_at DESC", projectID.Value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Webhook
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *WebhookRepository) Disable(ctx context.Context, id domain.WebhookID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE webhooks SET disabled_at = now() WHERE id = $1 AND disabled_at IS NULL`, id.Value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return application.ErrWebhookNotFound
	}
	return nil
}

func scanWebhook(row pgx.Row) (domain.Webhook, error) {
	var w domain.Webhook
	var createdBy *string
	if err := row.Scan(&w.ID.Value, &w.ProjectID.Value, &w.Name, &w.URL, &w.Events, &w.EncryptedSecret, &w.CreatedAt, &createdBy, &w.DisabledAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Webhook{}, application.ErrWebhookNotFound
		}
		return domain.Webhook{}, err
	}
	if createdBy != nil {
		w.CreatedBy = *createdBy
	}
	return w, nil
}

// --- deliveries (outbox) ---

type WebhookDeliveryRepository struct{ pool *pgxpool.Pool }

func NewWebhookDeliveryRepository(pool *pgxpool.Pool) *WebhookDeliveryRepository {
	return &WebhookDeliveryRepository{pool: pool}
}

const deliveryColumns = `id, webhook_id, event, payload, status, attempt, available_at, last_status_code, last_error, created_at, delivered_at`

func (r *WebhookDeliveryRepository) Insert(ctx context.Context, d domain.WebhookDelivery) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO webhook_deliveries (id, webhook_id, event, payload, status, attempt, available_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		d.ID, d.WebhookID.Value, d.Event, string(d.Payload), string(d.Status), d.Attempt, d.AvailableAt, d.CreatedAt)
	return err
}

// ClaimDue picks due rows (PENDING/RETRY whose time has come, or SENDING whose lease expired —
// a sender that crashed mid-POST) with SKIP LOCKED, so concurrent senders never take the same
// row, and bumps attempt as part of the claim.
func (r *WebhookDeliveryRepository) ClaimDue(ctx context.Context, limit int, lease time.Duration) ([]domain.WebhookDelivery, error) {
	rows, err := r.pool.Query(ctx, `
		WITH due AS (
			SELECT id FROM webhook_deliveries
			 WHERE attempt < $3 AND (
			       (status IN ('PENDING','RETRY') AND available_at <= now())
			    OR (status = 'SENDING' AND lease_until < now()))
			 ORDER BY available_at
			 LIMIT $1
			 FOR UPDATE SKIP LOCKED)
		UPDATE webhook_deliveries d
		   SET status = 'SENDING', attempt = d.attempt + 1, lease_until = now() + make_interval(secs => $2)
		  FROM due WHERE d.id = due.id
		RETURNING d.id, d.webhook_id, d.event, d.payload, d.status, d.attempt, d.available_at,
		          d.last_status_code, d.last_error, d.created_at, d.delivered_at`,
		limit, lease.Seconds(), domain.WebhookMaxAttempts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.WebhookDelivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *WebhookDeliveryRepository) MarkDelivered(ctx context.Context, d domain.WebhookDelivery, statusCode int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		   SET status = 'DELIVERED', delivered_at = now(), last_status_code = $2, last_error = NULL, lease_until = NULL
		 WHERE id = $1`, d.ID, statusCode)
	return err
}

func (r *WebhookDeliveryRepository) MarkFailed(ctx context.Context, d domain.WebhookDelivery, statusCode *int, message string) error {
	status := domain.DeliveryRetry
	if d.Attempt >= domain.WebhookMaxAttempts {
		status = domain.DeliveryDead
	}
	if len(message) > 1000 {
		message = message[:1000]
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		   SET status = $2, last_status_code = $3, last_error = $4, lease_until = NULL,
		       available_at = now() + make_interval(secs => $5)
		 WHERE id = $1`, d.ID, string(status), statusCode, message, domain.WebhookRetryDelay(d.Attempt).Seconds())
	return err
}

func (r *WebhookDeliveryRepository) LatestByWebhook(ctx context.Context, webhookID domain.WebhookID) (domain.WebhookDelivery, bool, error) {
	d, err := scanDelivery(r.pool.QueryRow(ctx, `SELECT `+deliveryColumns+` FROM webhook_deliveries
		WHERE webhook_id = $1 ORDER BY created_at DESC LIMIT 1`, webhookID.Value))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.WebhookDelivery{}, false, nil
		}
		return domain.WebhookDelivery{}, false, err
	}
	return d, true, nil
}

func scanDelivery(row pgx.Row) (domain.WebhookDelivery, error) {
	var d domain.WebhookDelivery
	var status string
	var payload string
	if err := row.Scan(&d.ID, &d.WebhookID.Value, &d.Event, &payload, &status, &d.Attempt, &d.AvailableAt,
		&d.LastStatusCode, &d.LastError, &d.CreatedAt, &d.DeliveredAt); err != nil {
		return domain.WebhookDelivery{}, err
	}
	d.Payload = []byte(payload)
	d.Status = domain.WebhookDeliveryStatus(status)
	return d, nil
}
