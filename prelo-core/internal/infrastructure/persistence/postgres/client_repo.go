package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// ClientRepository persists Fase C1 clients and their contacts.
type ClientRepository struct{ pool *pgxpool.Pool }

func NewClientRepository(pool *pgxpool.Pool) *ClientRepository {
	return &ClientRepository{pool: pool}
}

const clientColumns = `c.id, c.name, c.company, c.status, c.stage, c.source, c.address, c.city, c.website,
	c.external_ref, c.notes, c.opted_out_at, c.created_at, c.created_by, c.updated_at, c.client_version`

func (r *ClientRepository) Insert(ctx context.Context, c domain.Client) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO clients (id, name, company, status, stage, source, address, city, website, external_ref, notes,
		                     created_at, created_by, updated_at, client_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		c.ID.Value, c.Name, c.Company, string(c.Status), string(c.Stage), string(c.Source), c.Address, c.City, c.Website,
		c.ExternalRef, c.Notes, c.CreatedAt, nullString(c.CreatedBy), c.UpdatedAt, c.Version)
	return err
}

func (r *ClientRepository) FindByID(ctx context.Context, id domain.ClientID) (domain.Client, error) {
	return scanClient(r.pool.QueryRow(ctx, `SELECT `+clientColumns+` FROM clients c WHERE c.id = $1 AND c.deleted_at IS NULL`, id.Value))
}

func (r *ClientRepository) List(ctx context.Context, filter application.ClientListFilter) ([]application.ClientSummary, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var status *string
	if filter.Status != nil {
		s := string(*filter.Status)
		status = &s
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+clientColumns+`,
		       pc.id, pc.kind, pc.value, pc.is_primary, pc.created_at,
		       (SELECT count(*) FROM projects p WHERE p.client_id = c.id AND p.deleted_at IS NULL)
		  FROM clients c
		  LEFT JOIN LATERAL (
		       SELECT id, kind, value, is_primary, created_at FROM client_contacts
		        WHERE client_id = c.id ORDER BY is_primary DESC, created_at LIMIT 1) pc ON true
		 WHERE c.deleted_at IS NULL AND ($1::text IS NULL OR c.status = $1)
		 ORDER BY c.updated_at DESC
		 LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []application.ClientSummary{}
	for rows.Next() {
		var s application.ClientSummary
		var contactID *uuid.UUID
		var kind, value *string
		var isPrimary *bool
		var contactCreated *time.Time
		dest, finish := clientScan(&s.Client)
		if err := rows.Scan(append(dest, &contactID, &kind, &value, &isPrimary, &contactCreated, &s.ProjectCount)...); err != nil {
			return nil, err
		}
		finish()
		if contactID != nil {
			s.PrimaryContact = &domain.ClientContact{ID: *contactID, ClientID: s.Client.ID, Kind: domain.ContactKind(*kind), Value: *value, IsPrimary: *isPrimary}
			if contactCreated != nil {
				s.PrimaryContact.CreatedAt = *contactCreated
			}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *ClientRepository) Update(ctx context.Context, c domain.Client) (domain.Client, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE clients
		   SET name = $2, company = $3, status = $4, stage = $5, address = $6, city = $7, website = $8, notes = $9,
		       updated_at = now(), client_version = client_version + 1
		 WHERE id = $1 AND client_version = $10 AND deleted_at IS NULL
		 RETURNING client_version, updated_at`,
		c.ID.Value, c.Name, c.Company, string(c.Status), string(c.Stage), c.Address, c.City, c.Website, c.Notes, c.Version)
	if err := row.Scan(&c.Version, &c.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Client{}, application.ErrOptimisticLock
		}
		return domain.Client{}, err
	}
	return c, nil
}

// SoftDelete hides the client, unlinks its projects and frees its contacts (so the same number
// can be registered again for another client).
func (r *ClientRepository) SoftDelete(ctx context.Context, id domain.ClientID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE clients SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id.Value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return application.ErrClientNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM client_contacts WHERE client_id = $1`, id.Value); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE projects SET client_id = NULL WHERE client_id = $1`, id.Value); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *ClientRepository) AddContact(ctx context.Context, c domain.ClientContact) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if c.IsPrimary {
		if _, err := tx.Exec(ctx, `UPDATE client_contacts SET is_primary = false WHERE client_id = $1`, c.ClientID.Value); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO client_contacts (id, client_id, kind, value, is_primary, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`, c.ID, c.ClientID.Value, string(c.Kind), c.Value, c.IsPrimary, c.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return application.ErrContactInUse
		}
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE clients SET updated_at = now() WHERE id = $1`, c.ClientID.Value); err != nil {
		return err
	}
	// Fase C2: conversations that already came from this number/WhatsApp ID join the client.
	if c.Kind == domain.ContactKindPhone || c.Kind == domain.ContactKindWhatsApp {
		if _, err := tx.Exec(ctx, `UPDATE tasks SET client_id = $1 WHERE client_id IS NULL AND contact_address = ANY($2)`,
			c.ClientID.Value, domain.ContactMatchKeys(c.Value)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *ClientRepository) FindByContactKeys(ctx context.Context, keys []string) (*domain.ClientID, error) {
	var id uuid.UUID
	err := r.pool.QueryRow(ctx, `
		SELECT c.id FROM client_contacts cc JOIN clients c ON c.id = cc.client_id
		 WHERE cc.kind IN ('PHONE', 'WHATSAPP') AND cc.value = ANY($1) AND c.deleted_at IS NULL
		 ORDER BY (cc.kind = 'WHATSAPP') DESC, cc.created_at
		 LIMIT 1`, keys).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &domain.ClientID{Value: id}, nil
}

func (r *ClientRepository) RemoveContact(ctx context.Context, clientID domain.ClientID, contactID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM client_contacts WHERE id = $1 AND client_id = $2`, contactID, clientID.Value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return application.ErrContactNotFound
	}
	return nil
}

func (r *ClientRepository) ListContacts(ctx context.Context, clientID domain.ClientID) ([]domain.ClientContact, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, kind, value, is_primary, created_at FROM client_contacts
		 WHERE client_id = $1 ORDER BY is_primary DESC, created_at`, clientID.Value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ClientContact{}
	for rows.Next() {
		c := domain.ClientContact{ClientID: clientID}
		var kind string
		if err := rows.Scan(&c.ID, &kind, &c.Value, &c.IsPrimary, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Kind = domain.ContactKind(kind)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *ClientRepository) SetProjectClient(ctx context.Context, projectID domain.ProjectID, clientID *domain.ClientID) error {
	var value *uuid.UUID
	if clientID != nil {
		value = &clientID.Value
		if _, err := r.FindByID(ctx, *clientID); err != nil {
			return err
		}
	}
	tag, err := r.pool.Exec(ctx, `UPDATE projects SET client_id = $2 WHERE id = $1 AND deleted_at IS NULL`, projectID.Value, value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return application.ErrProjectNotFound
	}
	return nil
}

func (r *ClientRepository) ListProjects(ctx context.Context, clientID domain.ClientID) ([]domain.Project, error) {
	rows, err := r.pool.Query(ctx, projectSelect+" AND client_id = $1 ORDER BY created_at DESC", clientID.Value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *ClientRepository) ClientOfProject(ctx context.Context, projectID domain.ProjectID) (*domain.ClientID, error) {
	var id *uuid.UUID
	err := r.pool.QueryRow(ctx, `
		SELECT p.client_id FROM projects p
		  LEFT JOIN clients c ON c.id = p.client_id AND c.deleted_at IS NULL
		 WHERE p.id = $1 AND (p.client_id IS NULL OR c.id IS NOT NULL)`, projectID.Value).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if id == nil {
		return nil, nil
	}
	return &domain.ClientID{Value: *id}, nil
}

// clientScan returns Scan destinations for clientColumns plus a finisher that copies the
// nullable created_by into the struct once Scan has run.
func clientScan(c *domain.Client) ([]any, func()) {
	var createdBy *string
	dest := []any{&c.ID.Value, &c.Name, &c.Company, (*string)(&c.Status), (*string)(&c.Stage), (*string)(&c.Source),
		&c.Address, &c.City, &c.Website, &c.ExternalRef, &c.Notes, &c.OptedOutAt, &c.CreatedAt, &createdBy, &c.UpdatedAt, &c.Version}
	return dest, func() {
		if createdBy != nil {
			c.CreatedBy = *createdBy
		}
	}
}

func scanClient(row pgx.Row) (domain.Client, error) {
	var c domain.Client
	dest, finish := clientScan(&c)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Client{}, application.ErrClientNotFound
		}
		return domain.Client{}, err
	}
	finish()
	return c, nil
}
