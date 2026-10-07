package runtime

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/exotermo/prelo-executor-worker/internal/resources"
)

type fakeRunner struct {
	calls   [][]string
	output  []byte
	err     error
	results []runnerResult
}

type runnerResult struct {
	output []byte
	err    error
}

func (f *fakeRunner) Run(_ context.Context, _ io.Reader, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	if i := len(f.calls) - 1; i < len(f.results) {
		return f.results[i].output, f.results[i].err
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.output != nil {
		return f.output, nil
	}
	for _, arg := range args {
		if arg == "ps" {
			return []byte(""), nil
		}
	}
	return []byte("container-id"), nil
}

func TestPodmanOOMStopsOnlyTheAffectedWorkspace(t *testing.T) {
	id := "ea776597-ddb6-4eb0-96ca-831a40597344"
	f := &fakeRunner{results: []runnerResult{{err: errors.New("exec failed")}, {output: []byte("true\n")}, {}}}
	p, err := NewPodman(f, "localhost/prelo-workspace@sha256:"+strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.RunOperation(context.Background(), id, OperationList, ".", nil)
	var limitErr *ResourceLimitError
	if !errors.As(err, &limitErr) || limitErr.Resource != "memory" {
		t.Fatalf("OOM was not classified: %v", err)
	}
	if len(f.calls) != 3 || !strings.Contains(strings.Join(f.calls[2], " "), "rm --force --ignore prelo-task-"+id) {
		t.Fatalf("expected cleanup of only this task container: %+v", f.calls)
	}
}

func TestPodmanActiveCountCountsOnlyWellFormedRuntimeIDs(t *testing.T) {
	f := &fakeRunner{output: []byte("ea776597-ddb6-4eb0-96ca-831a40597344\n88c51e72-7356-4c2f-b994-39694e40870a\n")}
	p, err := NewPodman(f, "localhost/prelo-workspace@sha256:"+strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.ActiveCount(context.Background())
	if err != nil || got != 2 {
		t.Fatalf("active count=%d, err=%v", got, err)
	}
	joined := strings.Join(f.calls[0], " ")
	if !strings.Contains(joined, "ps --filter label=prelo.execution_id --format={{.Label \"prelo.execution_id\"}}") {
		t.Fatalf("unexpected Podman query: %s", joined)
	}
}

func TestPodmanActiveCountFailsClosedOnMalformedOutput(t *testing.T) {
	f := &fakeRunner{output: []byte("container-id\n")}
	p, err := NewPodman(f, "localhost/prelo-workspace@sha256:"+strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := p.ActiveCount(context.Background()); err == nil || got != 0 {
		t.Fatalf("malformed runtime output was accepted: count=%d err=%v", got, err)
	}
}

func TestPodmanStartHasClosedIsolationProfile(t *testing.T) {
	f := &fakeRunner{}
	digest := "sha256:" + strings.Repeat("a", 64)
	p, err := NewPodman(f, "localhost/prelo-workspace@"+digest)
	if err != nil {
		t.Fatal(err)
	}
	id := "ea776597-ddb6-4eb0-96ca-831a40597344"
	if err := p.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 {
		t.Fatal("start was not sent")
	}
	joined := " " + strings.Join(f.calls[1], " ") + " "
	for _, required := range []string{" --network=none ", " --cap-drop=all ", " --security-opt=no-new-privileges ", " --read-only ", " --pull=never ", " --pids-limit=64 ", " --memory=268435456b ", " --memory-swap=268435456b ", " --cpus=0.5 ", " --userns=auto ", " --tmpfs=/workspace:", " --entrypoint=/workspace-helper ", " localhost/prelo-workspace@" + digest + " "} {
		if !strings.Contains(joined, required) {
			t.Errorf("missing isolation option %s", required)
		}
	}
	for _, forbidden := range []string{"--volume", "--mount", "--privileged", "--device", "--network=host", "--pid=host", "--ipc=host", "--env", "--replace"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("unsafe option %s", forbidden)
		}
	}
	if err := p.Stop(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 3 || f.calls[2][0] != "rm" {
		t.Fatal("stop was not sent")
	}
}

func TestPodmanUsesConfiguredStorageRootAndResourceProfile(t *testing.T) {
	f := &fakeRunner{}
	storage := t.TempDir()
	profile := resources.Profile{MemoryBytes: 384 << 20, CPU: .75, DiskBytes: 1 << 30, PIDs: 96}
	p, err := NewPodmanWithConfig(f, "localhost/prelo-workspace@sha256:"+strings.Repeat("a", 64), storage, profile)
	if err != nil {
		t.Fatal(err)
	}
	id := "ea776597-ddb6-4eb0-96ca-831a40597344"
	if err := p.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	joined := " " + strings.Join(f.calls[1], " ") + " "
	for _, want := range []string{" --root=" + storage + " ", " --memory=402653184b ", " --memory-swap=402653184b ", " --cpus=0.75 ", " --pids-limit=96 "} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing configured option %q in %s", want, joined)
		}
	}
}

func TestPodmanStartDoesNotDuplicateAnExistingExecutionContainer(t *testing.T) {
	id := "ea776597-ddb6-4eb0-96ca-831a40597344"
	f := &fakeRunner{output: []byte(id + "\n")}
	p, err := NewPodman(f, "localhost/prelo-workspace@sha256:"+strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Start(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("duplicate start should not spawn a container: %d commands", len(f.calls))
	}
}

func TestPodmanRejectsUnpinnedImageAndUntrustedExecutionID(t *testing.T) {
	f := &fakeRunner{}
	if _, err := NewPodman(f, "docker.io/library/alpine:latest"); err == nil {
		t.Fatal("accepted mutable image")
	}
	p, _ := NewPodman(f, "localhost/prelo-workspace@sha256:"+strings.Repeat("a", 64))
	if err := p.Start(context.Background(), "anything; rm -rf /"); err == nil {
		t.Fatal("accepted invalid execution ID")
	}
	if len(f.calls) != 0 {
		t.Fatal("called Podman on invalid input")
	}
}

func TestPodmanRunsOnlyStructuredHelperOperations(t *testing.T) {
	f := &fakeRunner{}
	p, _ := NewPodman(f, "localhost/prelo-workspace@sha256:"+strings.Repeat("a", 64))
	id := "ea776597-ddb6-4eb0-96ca-831a40597344"
	if _, err := p.RunOperation(context.Background(), id, Operation("sh"), "x", nil); err == nil {
		t.Fatal("arbitrary command accepted")
	}
	if _, err := p.RunOperation(context.Background(), id, OperationCreate, "../escape", strings.NewReader("x")); err == nil {
		t.Fatal("path escape accepted")
	}
	if len(f.calls) != 0 {
		t.Fatal("invoked runtime for unsafe command")
	}
	if _, err := p.RunOperation(context.Background(), id, OperationCreate, "dir/note.txt", strings.NewReader("text")); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(f.calls[0], " ")
	if !strings.Contains(joined, "exec --user=10001:10001 --interactive prelo-task-"+id+" /workspace-helper create dir/note.txt") {
		t.Fatalf("unexpected exec: %s", joined)
	}
}
