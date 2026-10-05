package realtime

import (
	"testing"

	"github.com/google/uuid"
)

func TestHubFiltersAndNeverBlocks(t *testing.T) {
	h := NewHub()
	mine := uuid.New()
	visible, cancelA := h.Subscribe(func(e Event) bool { return e.ProjectID == nil || *e.ProjectID == mine })
	defer cancelA()
	other := uuid.New()
	h.Publish(Event{Kind: "task", ID: "1", ProjectID: &other})
	h.Publish(Event{Kind: "task", ID: "2", ProjectID: &mine})
	h.Publish(Event{Kind: "task", ID: "3"})
	if got := (<-visible).ID; got != "2" {
		t.Fatalf("another project's event leaked: %s", got)
	}
	if got := (<-visible).ID; got != "3" {
		t.Fatalf("unassigned events are visible to everyone, got %s", got)
	}

	slow, cancelB := h.Subscribe(nil)
	for i := 0; i < subscriberBuffer+10; i++ { // nobody reads: must not block Publish
		h.Publish(Event{Kind: "task", ID: "x"})
	}
	for i := 0; i < subscriberBuffer; i++ {
		<-slow
	}
	h.Publish(Event{Kind: "task", ID: "after"})
	if e := <-slow; e.Kind != "resync" {
		t.Fatalf("a lagged subscriber must get resync first, got %+v", e)
	}
	cancelB()
	cancelB() // idempotent
	if h.Subscribers() != 1 {
		t.Fatalf("subscribers = %d", h.Subscribers())
	}
}
