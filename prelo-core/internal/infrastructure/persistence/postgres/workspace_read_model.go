package postgres

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// WorkspaceReadModel answers Fase C1's cross-aggregate reads: search, recents, pending work and
// a client's timeline. Every query applies the same project visibility rule: $all OR the row's
// project is in $projects (tasks without a project — the WhatsApp bucket — stay visible to all).
type WorkspaceReadModel struct{ pool *pgxpool.Pool }

func NewWorkspaceReadModel(pool *pgxpool.Pool) *WorkspaceReadModel {
	return &WorkspaceReadModel{pool: pool}
}

const maxRecentPerUser = 50

// likePattern escapes LIKE metacharacters so what the user types is matched literally.
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

// digitsPattern lets "98445-0529" find "+5541984450529"; nil when the query has too few digits
// to be a phone fragment.
func digitsPattern(q string) *string {
	var b strings.Builder
	for _, r := range q {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	if b.Len() < 4 {
		return nil
	}
	p := "%" + b.String() + "%"
	return &p
}

func visibilityArgs(v application.ProjectVisibility) (bool, []uuid.UUID) {
	ids := v.ProjectIDs
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return v.All, ids
}

func (m *WorkspaceReadModel) Search(ctx context.Context, query string, types application.SearchTypes, v application.ProjectVisibility, limit int) (application.SearchResults, error) {
	all, ids := visibilityArgs(v)
	pattern := likePattern(query)
	out := application.SearchResults{Clients: []application.SearchHit{}, Projects: []application.SearchHit{}, Tasks: []application.SearchHit{}, Files: []application.SearchHit{}}

	if types.Clients {
		hits, err := m.queryHits(ctx, `
			SELECT 'CLIENT', c.id, c.name,
			       concat_ws(' · ', c.company, c.city, (SELECT value FROM client_contacts WHERE client_id = c.id ORDER BY is_primary DESC, created_at LIMIT 1)),
			       NULL::uuid, c.id, c.status, c.updated_at
			  FROM clients c
			 WHERE c.deleted_at IS NULL
			   AND (search_norm(c.name || ' ' || coalesce(c.company, '') || ' ' || coalesce(c.city, '')) LIKE search_norm($1)
			        OR EXISTS (SELECT 1 FROM client_contacts cc WHERE cc.client_id = c.id
			                    AND (cc.value LIKE search_norm($1) OR ($2::text IS NOT NULL AND cc.value LIKE $2))))
			 ORDER BY (c.status = 'ACTIVE') DESC, c.updated_at DESC
			 LIMIT $3`, pattern, digitsPattern(query), limit)
		if err != nil {
			return out, err
		}
		out.Clients = hits
	}
	if types.Projects {
		hits, err := m.queryHits(ctx, `
			SELECT 'PROJECT', p.id, p.name, coalesce(c.name, p.description, ''), p.id, p.client_id, '', p.created_at
			  FROM projects p LEFT JOIN clients c ON c.id = p.client_id AND c.deleted_at IS NULL
			 WHERE p.deleted_at IS NULL AND ($2 OR p.id = ANY($3))
			   AND search_norm(p.name || ' ' || coalesce(p.description, '')) LIKE search_norm($1)
			 ORDER BY p.created_at DESC
			 LIMIT $4`, pattern, all, ids, limit)
		if err != nil {
			return out, err
		}
		out.Projects = hits
	}
	if types.Tasks {
		hits, err := m.queryHits(ctx, `
			SELECT 'TASK', t.id, left(t.description, 160), concat_ws(' · ', p.name, t.agent_id, lower(t.source)),
			       t.project_id, t.client_id, t.status, t.created_at
			  FROM tasks t LEFT JOIN projects p ON p.id = t.project_id
			 WHERE ($2 OR t.project_id IS NULL OR t.project_id = ANY($3))
			   AND search_norm(t.description) LIKE search_norm($1)
			 ORDER BY t.created_at DESC
			 LIMIT $4`, pattern, all, ids, limit)
		if err != nil {
			return out, err
		}
		out.Tasks = hits
	}
	if types.Files {
		hits, err := m.queryHits(ctx, `
			SELECT 'FILE', f.id, f.name, p.name, f.project_id, p.client_id, f.kind, f.created_at
			  FROM project_files f JOIN projects p ON p.id = f.project_id AND p.deleted_at IS NULL
			 WHERE f.deleted_at IS NULL AND ($2 OR f.project_id = ANY($3))
			   AND search_norm(f.name) LIKE search_norm($1)
			 ORDER BY f.created_at DESC
			 LIMIT $4`, pattern, all, ids, limit)
		if err != nil {
			return out, err
		}
		out.Files = hits
	}
	return out, nil
}

func (m *WorkspaceReadModel) queryHits(ctx context.Context, sql string, args ...any) ([]application.SearchHit, error) {
	rows, err := m.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []application.SearchHit{}
	for rows.Next() {
		var h application.SearchHit
		if err := rows.Scan(&h.Kind, &h.ID, &h.Title, &h.Subtitle, &h.ProjectID, &h.ClientID, &h.Status, &h.At); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func (m *WorkspaceReadModel) TouchRecent(ctx context.Context, userID domain.DashboardUserID, kind string, refID uuid.UUID) error {
	batch := &pgx.Batch{}
	batch.Queue(`
		INSERT INTO user_recent_items (user_id, kind, ref_id, viewed_at) VALUES ($1, $2, $3, now())
		ON CONFLICT (user_id, kind, ref_id) DO UPDATE SET viewed_at = now()`, userID.Value, kind, refID)
	batch.Queue(`
		DELETE FROM user_recent_items WHERE user_id = $1 AND (kind, ref_id) NOT IN (
		       SELECT kind, ref_id FROM user_recent_items WHERE user_id = $1 ORDER BY viewed_at DESC LIMIT $2)`,
		userID.Value, maxRecentPerUser)
	return m.pool.SendBatch(ctx, batch).Close()
}

func (m *WorkspaceReadModel) ListRecent(ctx context.Context, userID domain.DashboardUserID, v application.ProjectVisibility, limit int) ([]application.RecentItem, error) {
	all, ids := visibilityArgs(v)
	rows, err := m.pool.Query(ctx, `
		SELECT r.kind, r.ref_id, r.viewed_at,
		       coalesce(c.name, p.name, left(t.description, 160)),
		       coalesce(concat_ws(' · ', c.company, c.city), p.description, tp.name, ''),
		       coalesce(c.status, t.status, ''),
		       coalesce(p.id, t.project_id)
		  FROM user_recent_items r
		  LEFT JOIN clients c  ON r.kind = 'CLIENT'  AND c.id = r.ref_id AND c.deleted_at IS NULL
		  LEFT JOIN projects p ON r.kind = 'PROJECT' AND p.id = r.ref_id AND p.deleted_at IS NULL
		  LEFT JOIN tasks t    ON r.kind = 'TASK'    AND t.id = r.ref_id
		  LEFT JOIN projects tp ON tp.id = t.project_id
		 WHERE r.user_id = $1
		   AND (c.id IS NOT NULL
		        OR (p.id IS NOT NULL AND ($2 OR p.id = ANY($3)))
		        OR (t.id IS NOT NULL AND ($2 OR t.project_id IS NULL OR t.project_id = ANY($3))))
		 ORDER BY r.viewed_at DESC
		 LIMIT $4`, userID.Value, all, ids, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []application.RecentItem{}
	for rows.Next() {
		var it application.RecentItem
		if err := rows.Scan(&it.Kind, &it.ID, &it.ViewedAt, &it.Title, &it.Subtitle, &it.Status, &it.ProjectID); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ListPending: open approvals, root tasks queued/running, and root tasks whose latest execution
// failed since failedSince — newest first within each kind, approvals first.
func (m *WorkspaceReadModel) ListPending(ctx context.Context, v application.ProjectVisibility, failedSince time.Time, limit int) ([]application.PendingItem, error) {
	all, ids := visibilityArgs(v)
	rows, err := m.pool.Query(ctx, `
		SELECT kind, id, task_id, title, detail, project_id, project_name, at FROM (
		  SELECT 'APPROVAL' AS kind, ar.id, coalesce(t.id, act.id) AS task_id,
		         coalesce(left(t.description, 160),
		                  'Deploy de ' || (act.payload->>'repository') || '@' || left(act.payload->>'commitSha', 7) || ' → ' || (act.payload->>'environment')) AS title,
		         coalesce(tc.tool_name || ' · risco ' || lower(tc.risk_level), 'pedido externo · risco ' || lower(act.risk) || ' · código ' || ar.short_code) AS detail,
		         coalesce(t.project_id, act.project_id) AS project_id, p.name AS project_name, ar.requested_at AS at, 0 AS rank
		    FROM approval_requests ar
		    LEFT JOIN tool_calls tc ON tc.id = ar.tool_call_id
		    LEFT JOIN tasks t ON t.id = tc.task_id
		    LEFT JOIN action_requests act ON act.id = ar.action_request_id
		    LEFT JOIN projects p ON p.id = coalesce(t.project_id, act.project_id)
		   WHERE ar.status = 'PENDING' AND ar.expires_at > now()
		     AND (t.id IS NOT NULL OR act.id IS NOT NULL)
		     AND ($1 OR (act.id IS NULL AND t.project_id IS NULL) OR coalesce(t.project_id, act.project_id) = ANY($2))
		  UNION ALL
		  SELECT 'RUNNING', t.id, t.id, left(t.description, 160), t.agent_id || ' · ' || lower(t.status),
		         t.project_id, p.name, t.created_at, 1
		    FROM tasks t LEFT JOIN projects p ON p.id = t.project_id
		   WHERE t.parent_task_id IS NULL AND t.status IN ('QUEUED', 'RUNNING')
		     AND ($1 OR t.project_id IS NULL OR t.project_id = ANY($2))
		  UNION ALL
		  SELECT 'FAILED', t.id, t.id, left(t.description, 160), coalesce(left(e.error, 200), 'falhou'),
		         t.project_id, p.name, coalesce(e.completed_at, t.created_at), 2
		    FROM tasks t
		    LEFT JOIN projects p ON p.id = t.project_id
		    LEFT JOIN LATERAL (SELECT error, completed_at FROM task_executions
		                        WHERE task_id = t.id ORDER BY started_at DESC NULLS LAST LIMIT 1) e ON true
		   WHERE t.parent_task_id IS NULL AND t.status = 'FAILED'
		     AND coalesce(e.completed_at, t.created_at) >= $3
		     AND ($1 OR t.project_id IS NULL OR t.project_id = ANY($2))
		) x
		ORDER BY rank, at DESC
		LIMIT $4`, all, ids, failedSince, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []application.PendingItem{}
	for rows.Next() {
		var it application.PendingItem
		if err := rows.Scan(&it.Kind, &it.ID, &it.TaskID, &it.Title, &it.Detail, &it.ProjectID, &it.ProjectName, &it.At); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (m *WorkspaceReadModel) ClientTimeline(ctx context.Context, clientID domain.ClientID, v application.ProjectVisibility, before *time.Time, limit int) ([]application.TimelineEntry, error) {
	all, ids := visibilityArgs(v)
	rows, err := m.pool.Query(ctx, `
		WITH cp AS (SELECT id, name FROM projects WHERE client_id = $1 AND deleted_at IS NULL)
		SELECT kind, id, title, detail, status, project_id, at FROM (
		  SELECT 'CLIENT_CREATED' AS kind, c.id, c.name AS title, 'Cliente cadastrado (' || lower(c.source) || ')' AS detail,
		         c.status, NULL::uuid AS project_id, c.created_at AS at
		    FROM clients c WHERE c.id = $1
		  UNION ALL
		  SELECT 'PROJECT', p.id, p.name, coalesce(p.description, ''), '', p.id, p.created_at
		    FROM projects p WHERE p.client_id = $1 AND p.deleted_at IS NULL AND ($2 OR p.id = ANY($3))
		  UNION ALL
		  SELECT 'TASK', t.id, left(t.description, 200), concat_ws(' · ', cp.name, t.agent_id, lower(t.source)),
		         t.status, t.project_id, t.created_at
		    FROM tasks t LEFT JOIN cp ON cp.id = t.project_id
		   WHERE t.parent_task_id IS NULL
		     AND (t.client_id = $1 OR t.project_id IN (SELECT id FROM cp))
		     AND ($2 OR t.project_id IS NULL OR t.project_id = ANY($3))
		  UNION ALL
		  SELECT 'FILE', f.id, f.name, cp.name, f.kind, f.project_id, f.created_at
		    FROM project_files f JOIN cp ON cp.id = f.project_id
		   WHERE f.deleted_at IS NULL AND ($2 OR f.project_id = ANY($3))
		) x
		WHERE ($4::timestamptz IS NULL OR at < $4)
		ORDER BY at DESC
		LIMIT $5`, clientID.Value, all, ids, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []application.TimelineEntry{}
	for rows.Next() {
		var e application.TimelineEntry
		if err := rows.Scan(&e.Kind, &e.ID, &e.Title, &e.Detail, &e.Status, &e.ProjectID, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
