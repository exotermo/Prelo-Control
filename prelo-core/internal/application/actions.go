package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

// ActionRequestService is PR-3 (docs/integracoes/action-requests.md): an external system (the
// BastionDeploy) asks to perform one exact action; the owner decides through the same approval
// flow as agents' tools (dashboard, app, "SIM <código>" on WhatsApp); the system reports back.
type ActionRequestService struct {
	actions    ActionRequestRepository
	workspaces WorkspaceReader
	owner      *OwnerApprovalNotifier
	events     EventPublisher
	now        func() time.Time
}

type WorkspaceReader interface {
	Current(ctx context.Context) (domain.Workspace, error)
}

func NewActionRequestService(actions ActionRequestRepository, workspaces WorkspaceReader) *ActionRequestService {
	return &ActionRequestService{actions: actions, workspaces: workspaces, events: NoopEventPublisher{}, now: time.Now}
}

// SetNotifications wires the owner's WhatsApp and the project's webhooks (both optional).
func (s *ActionRequestService) SetNotifications(owner *OwnerApprovalNotifier, events EventPublisher) {
	s.owner = owner
	if events != nil {
		s.events = events
	}
}

type CreateActionInput struct {
	Kind           string
	ProjectID      domain.ProjectID
	Payload        json.RawMessage
	PayloadHash    string
	Impact         string
	RequestedBy    string
	IdempotencyKey string
}

// Create returns (view, created). The same idempotencyKey with the same payloadHash returns the
// existing request; with another payloadHash it is a conflict — nothing is overwritten.
func (s *ActionRequestService) Create(ctx context.Context, callerProject domain.ProjectID, apiKeyID *uuid.UUID, in CreateActionInput) (ActionView, bool, error) {
	if in.ProjectID != callerProject {
		return ActionView{}, false, ErrNotProjectMember
	}
	if existing, found, err := s.actions.FindByIdempotencyKey(ctx, in.ProjectID, strings.TrimSpace(in.IdempotencyKey)); err != nil {
		return ActionView{}, false, err
	} else if found {
		return s.sameOrConflict(existing, in.PayloadHash)
	}
	workspace, err := s.workspaces.Current(ctx)
	if err != nil {
		return ActionView{}, false, err
	}
	action, err := domain.NewActionRequest(workspace.ID, in.ProjectID, in.Kind, in.Payload, in.PayloadHash, in.Impact, in.RequestedBy, in.IdempotencyKey, apiKeyID)
	if err != nil {
		return ActionView{}, false, err
	}
	approval := domain.NewActionApproval(action.ID, ActionScope(action), defaultApprovalTTL)
	if err := s.actions.InsertWithApproval(ctx, action, approval); err != nil {
		if errors.Is(err, ErrIdempotencyConflict) { // lost a race with the same key
			if existing, found, findErr := s.actions.FindByIdempotencyKey(ctx, in.ProjectID, action.IdempotencyKey); findErr == nil && found {
				return s.sameOrConflict(existing, in.PayloadHash)
			}
		}
		return ActionView{}, false, err
	}
	view, err := s.actions.FindByID(ctx, action.ID) // re-read: the short code may have been regenerated
	if err != nil {
		return ActionView{}, false, err
	}
	if s.owner != nil {
		s.owner.NotifyAction(ctx, view)
	}
	pid := view.Action.ProjectID
	s.events.Publish(ctx, &pid, domain.EventApprovalPending, map[string]any{
		"actionRequestId": view.Action.ID.String(), "approvalId": view.Approval.ID.String(), "kind": view.Action.Kind,
		"description": ActionScope(view.Action),
	})
	return view, true, nil
}

func (s *ActionRequestService) sameOrConflict(existing ActionView, payloadHash string) (ActionView, bool, error) {
	if existing.Action.PayloadHash != payloadHash {
		return ActionView{}, false, ErrIdempotencyConflict
	}
	return existing, false, nil
}

// Get is the source of truth an executor checks before acting (another project's id = not found).
func (s *ActionRequestService) Get(ctx context.Context, callerProject domain.ProjectID, id uuid.UUID) (ActionView, error) {
	view, err := s.actions.FindByID(ctx, id)
	if err != nil {
		return ActionView{}, err
	}
	if view.Action.ProjectID != callerProject {
		return ActionView{}, ErrActionRequestNotFound
	}
	return view, nil
}

func (s *ActionRequestService) ListForProject(ctx context.Context, projectID domain.ProjectID, kind string, limit int) ([]ActionView, error) {
	return s.actions.ListByProject(ctx, projectID, kind, limit)
}

type ActionResultInput struct {
	Status     string
	Sequence   int
	Message    string
	URL        string
	Digest     string
	ReportedAt time.Time
}

var terminalResults = map[string]bool{"SUCCEEDED": true, "FAILED": true, "ROLLED_BACK": true, "CANCELLED": true}

// ReportResult records what the executor did. Only an APPROVED request accepts results, the
// first one only within ActionStartGrace of the approval deadline, and an older sequence is a
// no-op (retries are safe).
func (s *ActionRequestService) ReportResult(ctx context.Context, callerProject domain.ProjectID, id uuid.UUID, in ActionResultInput) (ActionView, error) {
	view, err := s.Get(ctx, callerProject, id)
	if err != nil {
		return ActionView{}, err
	}
	if !domain.ActionResultStatuses[in.Status] {
		return ActionView{}, &domain.ValidationError{Message: "status must be RUNNING, SUCCEEDED, FAILED, ROLLED_BACK or CANCELLED"}
	}
	if in.Sequence < 1 {
		return ActionView{}, &domain.ValidationError{Message: "sequence must start at 1 and grow with each report"}
	}
	if len(in.Message) > 500 || len(in.URL) > 500 || len(in.Digest) > 100 {
		return ActionView{}, &domain.ValidationError{Message: "message/url (500) or artifactDigest (100) too long"}
	}
	if in.URL != "" && !strings.HasPrefix(in.URL, "https://") && !strings.HasPrefix(in.URL, "http://") {
		return ActionView{}, &domain.ValidationError{Message: "url must be http(s)"}
	}
	now := s.now().UTC()
	if view.Approval.EffectiveStatus(now) != domain.ApprovalApproved {
		return ActionView{}, ErrActionNotApproved
	}
	if view.Action.Result == nil && now.After(view.Approval.ExpiresAt.Add(domain.ActionStartGrace)) {
		return ActionView{}, ErrActionWindowClosed
	}
	if view.Action.Result != nil && in.Sequence <= view.Action.Result.Sequence {
		return view, nil
	}
	if in.ReportedAt.IsZero() {
		in.ReportedAt = now
	}
	result := domain.ActionResult{Status: in.Status, Sequence: in.Sequence, ReportedAt: in.ReportedAt,
		Message: nonEmpty(in.Message), URL: nonEmpty(in.URL), Digest: nonEmpty(in.Digest)}
	if err := s.actions.RecordResult(ctx, id, result); err != nil {
		return ActionView{}, err
	}
	updated, err := s.actions.FindByID(ctx, id)
	if err != nil {
		return ActionView{}, err
	}
	if terminalResults[in.Status] && s.owner != nil {
		s.owner.NotifyActionResult(ctx, updated)
	}
	return updated, nil
}

// OnApprovalDecided (called by DecideApprovalUseCase for action approvals) tells the requesting
// system through the project's webhooks. It is only a hint: the executor confirms with Get.
func (s *ActionRequestService) OnApprovalDecided(ctx context.Context, approval domain.ApprovalRequest) {
	if approval.ActionRequestID == nil {
		return
	}
	view, err := s.actions.FindByID(ctx, *approval.ActionRequestID)
	if err != nil {
		log.Printf("actions: decided approval %s has no readable request: %v", approval.ID, err)
		return
	}
	data := map[string]any{
		"actionRequestId": view.Action.ID.String(), "status": string(approval.EffectiveStatus(s.now().UTC())),
		"payloadHash": view.Action.PayloadHash,
	}
	if approval.DecidedAt != nil {
		data["decidedAt"] = approval.DecidedAt.UTC().Format(time.RFC3339)
	}
	if approval.DecidedBy != nil {
		data["decidedBy"] = *approval.DecidedBy
	}
	pid := view.Action.ProjectID
	s.events.Publish(ctx, &pid, domain.EventActionDecided, data)
}

// ActionScope is the plain-language description shown wherever the approval appears.
func ActionScope(a domain.ActionRequest) string {
	if a.Kind == domain.ActionKindDeploy {
		d := a.DeployOf()
		return fmt.Sprintf("Deploy de %s@%s (%s) em %s → %s · pedido por %s · impacto: %s",
			d.Repository, shortSha(d.CommitSha), d.App, d.Environment, d.Target, a.RequestedBy, a.Impact)
	}
	return fmt.Sprintf("%s pedido por %s · impacto: %s", a.Kind, a.RequestedBy, a.Impact)
}

func shortSha(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func nonEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}
