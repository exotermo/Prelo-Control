package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const testToken = "prw_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestClientRejectsUnsafeOrigin(t *testing.T) {
	for _, raw := range []string{"http://prelo.example", "https://user:pass@prelo.example", "https://prelo.example/other", "https://prelo.example?token=x"} {
		if _, err := newPreloClient(raw, testToken, false); err == nil {
			t.Errorf("accepted unsafe origin %q", raw)
		}
	}
	if _, err := newPreloClient("https://prelo.example", testToken, false); err != nil {
		t.Fatal(err)
	}
}

func TestHeartbeatSendsBearerOnlyToConfiguredOriginAndFailsClosed(t *testing.T) {
	c, err := newPreloClient("https://prelo.example", testToken, false)
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://prelo.example/api/v1/executor-workers/heartbeat" || r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Fatal("unexpected heartbeat request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"workerId":"w","projectId":"p","canExecute":false,"status":"REGISTERED_NO_RUNTIME"}`)), Header: make(http.Header)}, nil
	})
	if _, err := c.heartbeat(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"workerId":"w","projectId":"p","canExecute":true,"status":"READY"}`)), Header: make(http.Header)}, nil
	})
	if response, err := c.heartbeat(context.Background()); err != nil || !response.CanExecute {
		t.Fatalf("ready response was rejected: %v", err)
	}
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})
	if _, err := c.heartbeat(context.Background()); !errors.Is(err, errRevoked) {
		t.Fatalf("revocation was ignored: %v", err)
	}
}

func TestClaimSendsCapacitySnapshotWithoutLeasingWhenServerHasNoReadyJob(t *testing.T) {
	c, err := newPreloClient("https://prelo.example", testToken, false)
	if err != nil {
		t.Fatal(err)
	}
	executionID := "ea776597-ddb6-4eb0-96ca-831a40597344"
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/executor-workers/jobs/claim" || r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Fatal("unexpected claim request")
		}
		var body struct {
			Capacity executorClaimCapacity `json:"capacity"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Capacity.ProfileID != "workspace-small-v1" || body.Capacity.AvailableSlots != 0 || body.Capacity.MaximumSlots != 1 || body.Capacity.ActiveExecutionIDs[0] != executionID {
			t.Fatalf("wrong capacity snapshot: %+v", body.Capacity)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})
	job, err := c.claim(context.Background(), executorClaimCapacity{ProfileID: "workspace-small-v1", MemoryBytes: 256 << 20,
		CPUQuotaMilli: 500, DiskBytes: 1 << 30, PIDs: 64, AvailableSlots: 0, MaximumSlots: 1,
		ActiveContainers: 1, ActiveExecutionIDs: []string{executionID}})
	if err != nil || job != nil {
		t.Fatalf("no-capacity poll should not hold a job lease: job=%+v err=%v", job, err)
	}
}
