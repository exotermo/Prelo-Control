package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/realtime"
)

// EventsHandler is PR-4 (contratos G4): GET /api/v1/events/stream — Server-Sent Events with change
// hints for a person's session (web or app). Authorization is the same as any request (Bearer;
// browsers use fetch streaming, not EventSource, so no token ever goes in the URL). Visibility is
// re-checked periodically, and an app session that gets revoked loses the stream.
type EventsHandler struct {
	hub      *realtime.Hub
	members  eventsMembers
	sessions mobileSessionChecker
	mu       sync.Mutex
	perUser  map[string]int
}

type eventsMembers interface {
	ListProjectsForUser(ctx context.Context, userID domain.DashboardUserID) ([]domain.Project, error)
}

const (
	maxStreamsPerUser = 5
	streamHeartbeat   = 25 * time.Second
)

func NewEventsHandler(hub *realtime.Hub, members eventsMembers, sessions mobileSessionChecker) *EventsHandler {
	return &EventsHandler{hub: hub, members: members, sessions: sessions, perUser: map[string]int{}}
}

func RegisterEventsRoutes(mux *http.ServeMux, h *EventsHandler) {
	mux.HandleFunc("GET /api/v1/events/stream", h.Stream)
}

// visibility returns the filter for this person: ADMIN sees every project; others their own
// projects; unassigned work (no project) is visible to everyone, as on the Tasks page.
func (h *EventsHandler) visibility(ctx context.Context, identity AuthContext, userID domain.DashboardUserID) (realtime.Filter, error) {
	if identity.HasScope("projects:manage") {
		return func(realtime.Event) bool { return true }, nil
	}
	projects, err := h.members.ListProjectsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	allowed := map[uuid.UUID]bool{}
	for _, p := range projects {
		allowed[p.ID.Value] = true
	}
	return func(e realtime.Event) bool { return e.ProjectID == nil || allowed[*e.ProjectID] }, nil
}

func (h *EventsHandler) Stream(w http.ResponseWriter, r *http.Request) {
	identity, userID, ok := sessionUser(w, r)
	if !ok {
		return
	}
	flusher, canFlush := w.(http.Flusher)
	if !canFlush {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Code: "streaming_unsupported", Message: "streaming unsupported"})
		return
	}
	key := userID.Value.String()
	h.mu.Lock()
	if h.perUser[key] >= maxStreamsPerUser {
		h.mu.Unlock()
		writeJSON(w, http.StatusTooManyRequests, errorResponse{Code: "too_many_streams", Message: "close another tab or device first"})
		return
	}
	h.perUser[key]++
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.perUser[key]--
		if h.perUser[key] <= 0 {
			delete(h.perUser, key)
		}
		h.mu.Unlock()
	}()

	var current realtime.Filter
	var filterMu sync.RWMutex
	refresh := func() error {
		f, err := h.visibility(r.Context(), identity, userID)
		if err != nil {
			return err
		}
		filterMu.Lock()
		current = f
		filterMu.Unlock()
		return nil
	}
	if err := refresh(); err != nil {
		writeError(w, err)
		return
	}
	events, cancel := h.hub.Subscribe(func(e realtime.Event) bool {
		filterMu.RLock()
		defer filterMu.RUnlock()
		return e.Kind == "resync" || current(e)
	})
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "retry: 5000\nevent: ready\ndata: {}\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(streamHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, open := <-events:
			if !open {
				return
			}
			data, _ := json.Marshal(e)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Kind, data)
			flusher.Flush()
		case <-heartbeat.C:
			if identity.SessionID != "" && !h.sessionStillActive(r.Context(), identity) {
				fmt.Fprintf(w, "event: session_ended\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			_ = refresh() // membership may have changed; keep the last good filter on error
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (h *EventsHandler) sessionStillActive(ctx context.Context, identity AuthContext) bool {
	if h.sessions == nil {
		return false
	}
	id, err := uuid.Parse(identity.SessionID)
	if err != nil {
		return false
	}
	s, err := h.sessions.FindByID(ctx, id)
	return err == nil && s.Active(time.Now().UTC()) && s.UserID.Value.String() == identity.Subject
}
