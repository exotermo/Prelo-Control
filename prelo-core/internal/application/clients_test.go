package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

type fakeClientRepo struct {
	ClientRepository
	inserted   []domain.Client
	deleted    []domain.ClientID
	contactErr error
	contacts   []domain.ClientContact
}

func (f *fakeClientRepo) Insert(_ context.Context, c domain.Client) error {
	f.inserted = append(f.inserted, c)
	return nil
}
func (f *fakeClientRepo) AddContact(_ context.Context, c domain.ClientContact) error {
	if f.contactErr != nil {
		return f.contactErr
	}
	f.contacts = append(f.contacts, c)
	return nil
}
func (f *fakeClientRepo) SoftDelete(_ context.Context, id domain.ClientID) error {
	f.deleted = append(f.deleted, id)
	return nil
}

type fakeMembers struct {
	ProjectMemberRepository
	projects []domain.Project
}

func (f fakeMembers) ListProjectsForUser(context.Context, domain.DashboardUserID) ([]domain.Project, error) {
	return f.projects, nil
}

type fakeReads struct {
	WorkspaceReadModel
	searched   int
	visibility ProjectVisibility
	since      time.Time
}

func (f *fakeReads) Search(_ context.Context, _ string, _ SearchTypes, v ProjectVisibility, _ int) (SearchResults, error) {
	f.searched++
	f.visibility = v
	return SearchResults{}, nil
}
func (f *fakeReads) ListRecent(context.Context, domain.DashboardUserID, ProjectVisibility, int) ([]RecentItem, error) {
	return nil, nil
}
func (f *fakeReads) ListPending(_ context.Context, v ProjectVisibility, since time.Time, _ int) ([]PendingItem, error) {
	f.visibility = v
	f.since = since
	return nil, nil
}

func TestSearchVisibilityPerRole(t *testing.T) {
	member := domain.Project{ID: domain.NewProjectID()}
	reads := &fakeReads{}
	svc := NewWorkspaceService(&fakeClientRepo{}, fakeMembers{projects: []domain.Project{member}}, reads)
	ctx := context.Background()

	if _, err := svc.Search(ctx, Viewer{AllProjects: true}, "padaria", SearchTypes{}); err != nil || !reads.visibility.All {
		t.Fatalf("admin should see every project: %+v %v", reads.visibility, err)
	}
	if _, err := svc.Search(ctx, Viewer{UserID: domain.DashboardUserID{Value: uuid.New()}}, "padaria", SearchTypes{}); err != nil {
		t.Fatal(err)
	}
	if reads.visibility.All || len(reads.visibility.ProjectIDs) != 1 || reads.visibility.ProjectIDs[0] != member.ID.Value {
		t.Fatalf("operator should only see member projects: %+v", reads.visibility)
	}
}

func TestSearchIgnoresTooShortAndRejectsTooLong(t *testing.T) {
	reads := &fakeReads{}
	svc := NewWorkspaceService(&fakeClientRepo{}, fakeMembers{}, reads)
	res, err := svc.Search(context.Background(), Viewer{AllProjects: true}, " a ", SearchTypes{})
	if err != nil || reads.searched != 0 || res.Clients == nil {
		t.Fatalf("1-char query should short-circuit with empty groups: %+v %v", res, err)
	}
	long := make([]byte, MaxSearchQueryLength+1)
	for i := range long {
		long[i] = 'x'
	}
	if _, err := svc.Search(context.Background(), Viewer{AllProjects: true}, string(long), SearchTypes{}); err == nil {
		t.Fatal("over-long query should be rejected")
	}
}

func TestHomeUsesFailedWindow(t *testing.T) {
	reads := &fakeReads{}
	svc := NewWorkspaceService(&fakeClientRepo{}, fakeMembers{}, reads)
	fixed := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixed }
	if _, err := svc.Home(context.Background(), Viewer{AllProjects: true}); err != nil {
		t.Fatal(err)
	}
	if !reads.since.Equal(fixed.Add(-48 * time.Hour)) {
		t.Fatalf("failed window = %v", reads.since)
	}
}

func TestCreateClientValidatesContactsBeforeWriting(t *testing.T) {
	repo := &fakeClientRepo{}
	svc := NewWorkspaceService(repo, fakeMembers{}, &fakeReads{})
	_, err := svc.CreateClient(context.Background(), domain.ClientFields{Name: "Padaria"},
		[]ContactInput{{Kind: domain.ContactKindPhone, Value: "41 98445-0529"}, {Kind: domain.ContactKindEmail, Value: "não é e-mail"}}, "u")
	if err == nil || len(repo.inserted) != 0 {
		t.Fatalf("invalid contact must abort before insert: err=%v inserted=%d", err, len(repo.inserted))
	}
}

func TestCreateClientUndoesClientWhenContactIsTaken(t *testing.T) {
	repo := &fakeClientRepo{contactErr: ErrContactInUse}
	svc := NewWorkspaceService(repo, fakeMembers{}, &fakeReads{})
	_, err := svc.CreateClient(context.Background(), domain.ClientFields{Name: "Padaria"},
		[]ContactInput{{Kind: domain.ContactKindPhone, Value: "41 98445-0529"}}, "u")
	if err != ErrContactInUse || len(repo.deleted) != 1 {
		t.Fatalf("taken contact should undo the client: err=%v deleted=%d", err, len(repo.deleted))
	}
}

func TestFirstContactBecomesPrimary(t *testing.T) {
	repo := &fakeClientRepo{}
	svc := NewWorkspaceService(repo, fakeMembers{}, &fakeReads{})
	if _, err := svc.CreateClient(context.Background(), domain.ClientFields{Name: "Padaria"},
		[]ContactInput{{Kind: domain.ContactKindPhone, Value: "41 98445-0529"}, {Kind: domain.ContactKindEmail, Value: "a@b.com"}}, "u"); err != nil {
		t.Fatal(err)
	}
	if !repo.contacts[0].IsPrimary || repo.contacts[1].IsPrimary || repo.contacts[0].Value != "+5541984450529" {
		t.Fatalf("contacts = %+v", repo.contacts)
	}
}

func TestTouchRecentRejectsUnknownKind(t *testing.T) {
	svc := NewWorkspaceService(&fakeClientRepo{}, fakeMembers{}, &fakeReads{})
	if err := svc.TouchRecent(context.Background(), Viewer{}, "FILE", uuid.New()); err == nil {
		t.Fatal("FILE is not a recent kind")
	}
}
