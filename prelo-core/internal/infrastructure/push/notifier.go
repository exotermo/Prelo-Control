package push

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/infrastructure/realtime"
)

// AndroidChannelID is the notification channel the app must create (contract: integracoes/push.md).
const AndroidChannelID = "prelo_alerts"

// Sender delivers one message (FCMClient in production).
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Targets resolves who may hear about a project (same visibility as the event stream).
type Targets interface {
	PushTargets(ctx context.Context, projectID *uuid.UUID, now time.Time) ([]string, error)
	ForgetPushToken(ctx context.Context, token string) error
}

// Notifier turns real-time events into pushes: a new pending approval and a deploy that finished.
// Best effort — WhatsApp and the screens remain the source of truth, so a lost push loses nothing.
type Notifier struct {
	hub     *realtime.Hub
	targets Targets
	sender  Sender
	now     func() time.Time
}

func NewNotifier(hub *realtime.Hub, targets Targets, sender Sender) *Notifier {
	return &Notifier{hub: hub, targets: targets, sender: sender, now: time.Now}
}

var actionBodies = map[string]string{
	"SUCCEEDED":   "Um deploy terminou com sucesso.",
	"FAILED":      "Um deploy falhou.",
	"ROLLED_BACK": "Um deploy foi revertido.",
	"CANCELLED":   "Um deploy foi cancelado.",
}

// MessageFor returns the push for an event, or false when the event doesn't warrant one.
func MessageFor(e realtime.Event) (Message, bool) {
	data := map[string]string{"kind": e.Kind, "id": e.ID}
	if e.ProjectID != nil {
		data["projectId"] = e.ProjectID.String()
	}
	switch {
	case e.Kind == "approval" && e.Status == "PENDING":
		if e.ActionRequestID != nil {
			data["actionRequestId"] = *e.ActionRequestID
		}
		return Message{Title: "Prelo Control", Body: "Há uma aprovação esperando você.", Tag: "approval", Data: data}, true
	case e.Kind == "action" && actionBodies[e.Status] != "":
		data["status"] = e.Status
		return Message{Title: "Prelo Control", Body: actionBodies[e.Status], Tag: "action-" + e.ID, Data: data}, true
	}
	return Message{}, false
}

// Run consumes the hub until ctx ends.
func (n *Notifier) Run(ctx context.Context) {
	events, cancel := n.hub.Subscribe(func(e realtime.Event) bool {
		_, ok := MessageFor(e)
		return ok
	})
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case e, open := <-events:
			if !open {
				return
			}
			n.deliver(ctx, e)
		}
	}
}

func (n *Notifier) deliver(ctx context.Context, e realtime.Event) {
	msg, ok := MessageFor(e)
	if !ok {
		return
	}
	tokens, err := n.targets.PushTargets(ctx, e.ProjectID, n.now().UTC())
	if err != nil {
		log.Printf("push: listing targets for %s %s: %v", e.Kind, e.ID, err)
		return
	}
	sent, failed := 0, 0
	for _, token := range tokens {
		m := msg
		m.Token = token
		sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := n.sender.Send(sendCtx, m)
		cancel()
		switch {
		case err == nil:
			sent++
		case errors.Is(err, ErrUnregistered):
			_ = n.targets.ForgetPushToken(ctx, token)
		default:
			failed++
			log.Printf("push: %s %s: %v", e.Kind, e.ID, err) // never the token itself
		}
	}
	if sent+failed > 0 {
		log.Printf("push: %s %s → %d sent, %d failed", e.Kind, e.ID, sent, failed)
	}
}
