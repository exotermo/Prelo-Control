package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

var (
	ErrClientNotFound  = errors.New("client not found")
	ErrContactNotFound = errors.New("contact not found")
	// ErrContactInUse: a phone/e-mail already belongs to another client (Fase C2 routes WhatsApp
	// by it, so it must be unique).
	ErrContactInUse = errors.New("contact already belongs to a client")
)

// ClientSummary is a list row: the client plus what the grid card shows.
type ClientSummary struct {
	Client         domain.Client
	PrimaryContact *domain.ClientContact
	ProjectCount   int
}

type ClientListFilter struct {
	Status *domain.ClientStatus
	Limit  int
}

// ClientRepository persists Clients and their contacts (Fase C1).
type ClientRepository interface {
	Insert(ctx context.Context, client domain.Client) error
	FindByID(ctx context.Context, id domain.ClientID) (domain.Client, error)
	List(ctx context.Context, filter ClientListFilter) ([]ClientSummary, error)
	Update(ctx context.Context, client domain.Client) (domain.Client, error)
	SoftDelete(ctx context.Context, id domain.ClientID) error

	AddContact(ctx context.Context, contact domain.ClientContact) error
	RemoveContact(ctx context.Context, clientID domain.ClientID, contactID uuid.UUID) error
	ListContacts(ctx context.Context, clientID domain.ClientID) ([]domain.ClientContact, error)

	// SetProjectClient links (or with nil unlinks) a project to a client.
	SetProjectClient(ctx context.Context, projectID domain.ProjectID, clientID *domain.ClientID) error
	ListProjects(ctx context.Context, clientID domain.ClientID) ([]domain.Project, error)
	// ClientOfProject returns the client a project belongs to, if any (tasks inherit it).
	ClientOfProject(ctx context.Context, projectID domain.ProjectID) (*domain.ClientID, error)
}

// ProjectVisibility is who-sees-what for the read models: ADMIN (projects:manage) sees every
// project; anyone else only the projects they are a member of. Tasks without a project (the
// WhatsApp bucket) stay visible to everyone, as on the Tasks page.
type ProjectVisibility struct {
	All        bool
	ProjectIDs []uuid.UUID
}

type SearchHit struct {
	Kind      string // CLIENT | PROJECT | TASK | FILE
	ID        uuid.UUID
	Title     string
	Subtitle  string
	ProjectID *uuid.UUID
	ClientID  *uuid.UUID
	Status    string
	At        time.Time
}

type SearchResults struct {
	Clients  []SearchHit
	Projects []SearchHit
	Tasks    []SearchHit
	Files    []SearchHit
}

type SearchTypes struct{ Clients, Projects, Tasks, Files bool }

// RecentItem is a "continue where you left off" card, resolved to a current title.
type RecentItem struct {
	Kind      string // CLIENT | PROJECT | TASK
	ID        uuid.UUID
	Title     string
	Subtitle  string
	Status    string
	ProjectID *uuid.UUID
	ViewedAt  time.Time
}

// PendingItem is something waiting on a human: an approval, a running task, a recent failure.
type PendingItem struct {
	Kind        string // APPROVAL | RUNNING | FAILED
	ID          uuid.UUID
	TaskID      uuid.UUID
	Title       string
	Detail      string
	ProjectID   *uuid.UUID
	ProjectName *string
	At          time.Time
}

// TimelineEntry is one event in a client's history (newest first).
type TimelineEntry struct {
	Kind      string // CLIENT_CREATED | PROJECT | TASK | FILE
	ID        uuid.UUID
	Title     string
	Detail    string
	Status    string
	ProjectID *uuid.UUID
	At        time.Time
}

// WorkspaceReadModel backs search, the home screen and the client timeline — cross-aggregate
// read queries that all apply the same ProjectVisibility.
type WorkspaceReadModel interface {
	Search(ctx context.Context, query string, types SearchTypes, visibility ProjectVisibility, limit int) (SearchResults, error)
	TouchRecent(ctx context.Context, userID domain.DashboardUserID, kind string, refID uuid.UUID) error
	ListRecent(ctx context.Context, userID domain.DashboardUserID, visibility ProjectVisibility, limit int) ([]RecentItem, error)
	ListPending(ctx context.Context, visibility ProjectVisibility, failedSince time.Time, limit int) ([]PendingItem, error)
	ClientTimeline(ctx context.Context, clientID domain.ClientID, visibility ProjectVisibility, before *time.Time, limit int) ([]TimelineEntry, error)
}
