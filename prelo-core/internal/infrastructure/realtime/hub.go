// Package realtime is PR-4 (contratos G4): change hints from Postgres (LISTEN prelo_events) fanned
// out to the web dashboard and the app over Server-Sent Events. An event says *what* changed
// (kind, id, project, status) — never content; clients re-fetch through the authorized API.
package realtime

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const Channel = "prelo_events"

type Event struct {
	Kind            string     `json:"kind"`
	ID              string     `json:"id"`
	ProjectID       *uuid.UUID `json:"projectId"`
	Status          string     `json:"status"`
	TaskID          *string    `json:"taskId,omitempty"`
	ParentID        *string    `json:"parentId,omitempty"`
	ActionRequestID *string    `json:"actionRequestId,omitempty"`
	At              string     `json:"at"`
}

// Filter decides whether a subscriber may see an event (project visibility).
type Filter func(Event) bool

type subscriber struct {
	ch     chan Event
	filter Filter
	lagged bool
}

// Hub fans events out to subscribers. A slow subscriber never blocks the others: when its buffer
// is full it is marked lagged and gets a single "resync" (re-fetch everything) when it drains.
type Hub struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[*subscriber]struct{}{}} }

const subscriberBuffer = 64

func (h *Hub) Subscribe(filter Filter) (<-chan Event, func()) {
	s := &subscriber{ch: make(chan Event, subscriberBuffer), filter: filter}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s.ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[s]; ok {
			delete(h.subs, s)
			close(s.ch)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		if s.filter != nil && !s.filter(e) {
			continue
		}
		if s.lagged {
			select {
			case s.ch <- Event{Kind: "resync", At: e.At}:
				s.lagged = false
			default:
				continue
			}
		}
		select {
		case s.ch <- e:
		default:
			s.lagged = true
		}
	}
}

func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Listen keeps a dedicated connection on LISTEN prelo_events and publishes every notification,
// reconnecting with backoff. Runs until ctx is done.
func (h *Hub) Listen(ctx context.Context, pool *pgxpool.Pool) {
	backoff := time.Second
	for ctx.Err() == nil {
		if err := h.listenOnce(ctx, pool); err != nil && ctx.Err() == nil {
			log.Printf("realtime: listener stopped (%v); reconnecting in %s", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
	}
}

func (h *Hub) listenOnce(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+Channel); err != nil {
		return err
	}
	// Anything missed while disconnected is unknown: tell everyone to re-fetch.
	h.Publish(Event{Kind: "resync", At: time.Now().UTC().Format(time.RFC3339)})
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var e Event
		if err := json.Unmarshal([]byte(n.Payload), &e); err != nil {
			log.Printf("realtime: ignoring malformed notification: %v", err)
			continue
		}
		h.Publish(e)
	}
}
