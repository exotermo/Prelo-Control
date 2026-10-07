package dispatch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeAuthority struct {
	grant Grant
	err   error
	calls int
}

func (f *fakeAuthority) Current(_ context.Context, _ string) (Grant, error) {
	f.calls++
	return f.grant, f.err
}

type fakeRuntime struct{ calls int }

func (f *fakeRuntime) Start(context.Context, string) error { f.calls++; return nil }

func TestStartRequiresFreshMatchingPreloApproval(t *testing.T) {
	now := time.Date(2026, 10, 6, 20, 0, 0, 0, time.UTC)
	job := Assignment{JobID: "job", ProjectID: "project", WorkerID: "worker", ExecutionID: "execution", ImageDigest: "sha256:" + strings.Repeat("a", 64), PayloadHash: "sha256:" + strings.Repeat("b", 64)}
	job.Operation = "START_WORKSPACE"
	grant := Grant{JobID: job.JobID, ProjectID: job.ProjectID, WorkerID: job.WorkerID, ExecutionID: job.ExecutionID, ImageDigest: job.ImageDigest, PayloadHash: job.PayloadHash, Status: "APPROVED", Operation: "START_WORKSPACE", StartBefore: now.Add(time.Minute)}
	authority := &fakeAuthority{grant: grant}
	runtime := &fakeRuntime{}
	starter := NewStarter(authority, runtime, job.WorkerID, job.ProjectID, job.ImageDigest)
	starter.now = func() time.Time { return now }
	otherProject := job
	otherProject.ProjectID = "other"
	if err := starter.Start(context.Background(), otherProject); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("cross-project assignment accepted")
	}
	for _, status := range []string{"PENDING", "DENIED", "EXPIRED", ""} {
		authority.grant.Status = status
		if err := starter.Start(context.Background(), job); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("%s reached runtime: %v", status, err)
		}
	}
	authority.grant = grant
	authority.grant.PayloadHash = "sha256:" + strings.Repeat("c", 64)
	if err := starter.Start(context.Background(), job); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("payload mismatch accepted")
	}
	authority.grant = grant
	authority.grant.StartBefore = now
	if err := starter.Start(context.Background(), job); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("closed start window accepted")
	}
	authority.grant = grant
	authority.err = errors.New("network unavailable")
	if err := starter.Start(context.Background(), job); err == nil {
		t.Fatal("network failure reached runtime")
	}
	authority.err = nil
	if runtime.calls != 0 {
		t.Fatal("runtime used without authorization")
	}
	if err := starter.Start(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if authority.calls != 8 || runtime.calls != 1 {
		t.Fatalf("confirmation calls=%d runtime calls=%d", authority.calls, runtime.calls)
	}
}
