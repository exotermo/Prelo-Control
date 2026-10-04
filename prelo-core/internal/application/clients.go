package application

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

const (
	MinSearchQueryLength = 2
	MaxSearchQueryLength = 120
	searchHitsPerGroup   = 8
	recentLimit          = 12
	pendingLimit         = 30
	pendingFailedWindow  = 48 * time.Hour
	timelinePageSize     = 50
)

// Viewer is who is asking: the dashboard user and whether they administer every project.
type Viewer struct {
	UserID      domain.DashboardUserID
	AllProjects bool
}

// WorkspaceService is Fase C1's application layer: clients CRUD plus the read models (search,
// home, timeline), each resolving the viewer's project visibility the same way List does on
// the project grid (ADMIN: all; anyone else: projects they are a member of).
type WorkspaceService struct {
	clients ClientRepository
	members ProjectMemberRepository
	reads   WorkspaceReadModel
	now     func() time.Time
}

func NewWorkspaceService(clients ClientRepository, members ProjectMemberRepository, reads WorkspaceReadModel) *WorkspaceService {
	return &WorkspaceService{clients: clients, members: members, reads: reads, now: time.Now}
}

func (s *WorkspaceService) visibility(ctx context.Context, viewer Viewer) (ProjectVisibility, error) {
	if viewer.AllProjects {
		return ProjectVisibility{All: true}, nil
	}
	projects, err := s.members.ListProjectsForUser(ctx, viewer.UserID)
	if err != nil {
		return ProjectVisibility{}, err
	}
	ids := make([]uuid.UUID, 0, len(projects))
	for _, p := range projects {
		ids = append(ids, p.ID.Value)
	}
	return ProjectVisibility{ProjectIDs: ids}, nil
}

// --- clients ---

func (s *WorkspaceService) CreateClient(ctx context.Context, fields domain.ClientFields, contacts []ContactInput, createdBy string) (domain.Client, error) {
	client, err := domain.NewClient(fields, domain.ClientSourceManual, createdBy)
	if err != nil {
		return domain.Client{}, err
	}
	// Validate every contact before writing anything, so a typo doesn't leave a half client.
	parsed := make([]domain.ClientContact, 0, len(contacts))
	for i, in := range contacts {
		c, err := domain.NewClientContact(client.ID, in.Kind, in.Value, in.IsPrimary || i == 0)
		if err != nil {
			return domain.Client{}, err
		}
		parsed = append(parsed, c)
	}
	if err := s.clients.Insert(ctx, client); err != nil {
		return domain.Client{}, err
	}
	for _, c := range parsed {
		if err := s.clients.AddContact(ctx, c); err != nil {
			_ = s.clients.SoftDelete(ctx, client.ID)
			return domain.Client{}, err
		}
	}
	return client, nil
}

type ContactInput struct {
	Kind      domain.ContactKind
	Value     string
	IsPrimary bool
}

func (s *WorkspaceService) UpdateClient(ctx context.Context, id domain.ClientID, version int64, fields domain.ClientFields) (domain.Client, error) {
	client, err := s.clients.FindByID(ctx, id)
	if err != nil {
		return domain.Client{}, err
	}
	if client.Version != version {
		return domain.Client{}, ErrOptimisticLock
	}
	updated, err := client.WithFields(fields)
	if err != nil {
		return domain.Client{}, err
	}
	return s.clients.Update(ctx, updated)
}

func (s *WorkspaceService) AddContact(ctx context.Context, id domain.ClientID, in ContactInput) (domain.ClientContact, error) {
	if _, err := s.clients.FindByID(ctx, id); err != nil {
		return domain.ClientContact{}, err
	}
	c, err := domain.NewClientContact(id, in.Kind, in.Value, in.IsPrimary)
	if err != nil {
		return domain.ClientContact{}, err
	}
	return c, s.clients.AddContact(ctx, c)
}

// ClientProjects lists the client's projects the viewer may see.
func (s *WorkspaceService) ClientProjects(ctx context.Context, viewer Viewer, id domain.ClientID) ([]domain.Project, error) {
	projects, err := s.clients.ListProjects(ctx, id)
	if err != nil {
		return nil, err
	}
	vis, err := s.visibility(ctx, viewer)
	if err != nil {
		return nil, err
	}
	if vis.All {
		return projects, nil
	}
	allowed := map[uuid.UUID]bool{}
	for _, pid := range vis.ProjectIDs {
		allowed[pid] = true
	}
	out := []domain.Project{}
	for _, p := range projects {
		if allowed[p.ID.Value] {
			out = append(out, p)
		}
	}
	return out, nil
}

// --- read models ---

func (s *WorkspaceService) Search(ctx context.Context, viewer Viewer, query string, types SearchTypes) (SearchResults, error) {
	q := strings.TrimSpace(query)
	if len([]rune(q)) < MinSearchQueryLength {
		return SearchResults{Clients: []SearchHit{}, Projects: []SearchHit{}, Tasks: []SearchHit{}, Files: []SearchHit{}}, nil
	}
	if len(q) > MaxSearchQueryLength {
		return SearchResults{}, &domain.ValidationError{Message: "search query must be at most 120 characters"}
	}
	if types == (SearchTypes{}) {
		types = SearchTypes{Clients: true, Projects: true, Tasks: true, Files: true}
	}
	vis, err := s.visibility(ctx, viewer)
	if err != nil {
		return SearchResults{}, err
	}
	return s.reads.Search(ctx, q, types, vis, searchHitsPerGroup)
}

type Home struct {
	Recent  []RecentItem
	Pending []PendingItem
}

func (s *WorkspaceService) Home(ctx context.Context, viewer Viewer) (Home, error) {
	vis, err := s.visibility(ctx, viewer)
	if err != nil {
		return Home{}, err
	}
	recent, err := s.reads.ListRecent(ctx, viewer.UserID, vis, recentLimit)
	if err != nil {
		return Home{}, err
	}
	pending, err := s.reads.ListPending(ctx, vis, s.now().Add(-pendingFailedWindow), pendingLimit)
	if err != nil {
		return Home{}, err
	}
	return Home{Recent: recent, Pending: pending}, nil
}

var recentKinds = map[string]bool{"CLIENT": true, "PROJECT": true, "TASK": true}

func (s *WorkspaceService) TouchRecent(ctx context.Context, viewer Viewer, kind string, refID uuid.UUID) error {
	if !recentKinds[kind] {
		return &domain.ValidationError{Message: "kind must be CLIENT, PROJECT or TASK"}
	}
	return s.reads.TouchRecent(ctx, viewer.UserID, kind, refID)
}

func (s *WorkspaceService) Timeline(ctx context.Context, viewer Viewer, id domain.ClientID, before *time.Time) ([]TimelineEntry, error) {
	if _, err := s.clients.FindByID(ctx, id); err != nil {
		return nil, err
	}
	vis, err := s.visibility(ctx, viewer)
	if err != nil {
		return nil, err
	}
	return s.reads.ClientTimeline(ctx, id, vis, before, timelinePageSize)
}
