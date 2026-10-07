package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/exotermo/prelo-executor-worker/internal/resources"
	"github.com/exotermo/prelo-executor-worker/internal/workspace"
)

var executionIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var imagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9./:_-]*@sha256:[0-9a-f]{64}$`)
var ErrUnsafeConfig = errors.New("unsafe container configuration")

type ResourceLimitError struct{ Resource string }

func (e *ResourceLimitError) Error() string {
	return "workspace container exceeded its " + e.Resource + " limit"
}

type Operation string

const (
	OperationList   Operation = "list"
	OperationRead   Operation = "read"
	OperationMkdir  Operation = "mkdir"
	OperationCreate Operation = "create"
)

const maxCommandOutput = 128 << 10

type CommandRunner interface {
	Run(context.Context, io.Reader, ...string) ([]byte, error)
}

// ExecRunner runs Podman as the unprivileged worker account. Secrets from the worker process
// are deliberately omitted from the child's environment. A task container receives none.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
	if os.Geteuid() == 0 {
		return nil, ErrUnsafeConfig
	}
	cmd := exec.CommandContext(ctx, "podman", args...)
	cmd.Stdin = input
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	for _, name := range []string{"HOME", "XDG_RUNTIME_DIR"} {
		if value := os.Getenv(name); value != "" {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	var out cappedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("podman command failed: %w", err)
	}
	if out.overflow {
		return nil, fmt.Errorf("podman output exceeded limit")
	}
	return out.Bytes(), nil
}

type cappedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := maxCommandOutput - b.Len()
	if remaining <= 0 {
		b.overflow = true
		return n, nil
	}
	if n > remaining {
		b.overflow = true
		_, _ = b.Buffer.Write(p[:remaining])
		return n, nil
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

type Podman struct {
	runner      CommandRunner
	image       string
	storageRoot string
	profile     resources.Profile
}

func NewPodman(runner CommandRunner, image string) (*Podman, error) {
	return NewPodmanWithConfig(runner, image, "", resources.Profile{
		MemoryBytes: 256 << 20, CPU: .5, DiskBytes: 1 << 30, PIDs: 64,
	})
}

// NewPodmanWithConfig places image/container storage on the operator-selected data volume
// and applies the same per-container profile used by the capacity admission calculation.
func NewPodmanWithConfig(runner CommandRunner, image, storageRoot string, profile resources.Profile) (*Podman, error) {
	if !imagePattern.MatchString(image) {
		return nil, ErrUnsafeConfig
	}
	if runner == nil || profile.MemoryBytes < 96<<20 || profile.CPU <= 0 || profile.PIDs < 16 {
		return nil, ErrUnsafeConfig
	}
	if storageRoot != "" {
		if !filepath.IsAbs(storageRoot) {
			return nil, ErrUnsafeConfig
		}
		if err := os.MkdirAll(storageRoot, 0700); err != nil {
			return nil, ErrUnsafeConfig
		}
		info, err := os.Stat(storageRoot)
		if err != nil || !info.IsDir() {
			return nil, ErrUnsafeConfig
		}
	}
	return &Podman{runner: runner, image: image, storageRoot: storageRoot, profile: profile}, nil
}

func containerName(executionID string) (string, error) {
	if !executionIDPattern.MatchString(executionID) {
		return "", ErrUnsafeConfig
	}
	return "prelo-task-" + executionID, nil
}

// Start creates one container for the execution. No host mount, socket, device, network,
// writable image root or privilege flag is accepted from a job. The image must be preloaded
// on the dedicated VM and pinned by digest; --pull=never prevents network image retrieval.
func (p *Podman) Start(ctx context.Context, executionID string) error {
	name, err := containerName(executionID)
	if err != nil {
		return err
	}
	active, err := p.ActiveExecutions(ctx)
	if err != nil {
		return err
	}
	for _, activeID := range active {
		if activeID == executionID {
			return nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err = p.run(ctx, nil, startArgs(name, p.image, p.profile)...)
	if err != nil {
		return p.classifyResourceFailure(ctx, name, executionID, err)
	}
	return nil
}

func startArgs(name, image string, profile resources.Profile) []string {
	return []string{"run", "--detach", "--pull=never", "--name=" + name,
		"--network=none", "--cap-drop=all", "--security-opt=no-new-privileges",
		"--read-only", "--pids-limit=" + strconv.FormatInt(profile.PIDs, 10),
		"--memory=" + strconv.FormatInt(profile.MemoryBytes, 10) + "b",
		"--memory-swap=" + strconv.FormatInt(profile.MemoryBytes, 10) + "b",
		"--cpus=" + strconv.FormatFloat(profile.CPU, 'f', -1, 64),
		"--user=10001:10001", "--userns=auto", "--pid=private", "--ipc=private", "--uts=private", "--cgroupns=private",
		"--tmpfs=/workspace:rw,nosuid,nodev,noexec,size=64m,uid=10001,gid=10001,mode=0700",
		"--tmpfs=/tmp:rw,nosuid,nodev,noexec,size=16m,uid=10001,gid=10001,mode=0700",
		"--workdir=/workspace", "--entrypoint=/workspace-helper", "--label=prelo.execution_id=" + strings.TrimPrefix(name, "prelo-task-"),
		image, "hold"}
}

// Stop is idempotent once the authorized task has completed or been cancelled.
func (p *Podman) Stop(ctx context.Context, executionID string) error {
	name, err := containerName(executionID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err = p.run(ctx, nil, "rm", "--force", "--ignore", name)
	return err
}

// ActiveCount counts only running Prelo workspace containers visible to this rootless Podman
// storage root. A malformed runtime response is an error, never interpreted as zero containers.
func (p *Podman) ActiveCount(ctx context.Context) (int, error) {
	ids, err := p.ActiveExecutions(ctx)
	return len(ids), err
}

// ActiveExecutions returns the execution IDs on running Prelo workspace containers. This lets
// the scheduler continue an existing workspace even when there is no capacity for another one.
func (p *Podman) ActiveExecutions(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := p.run(ctx, nil, "ps", "--filter", "label=prelo.execution_id", "--format={{.Label \"prelo.execution_id\"}}")
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		executionID := strings.TrimSpace(line)
		if executionID == "" {
			continue
		}
		if !executionIDPattern.MatchString(executionID) {
			return nil, fmt.Errorf("invalid execution label from Podman")
		}
		ids = append(ids, executionID)
	}
	return ids, nil
}

// RunOperation is a narrow transport to the trusted in-image helper. Its caller must first
// verify a live Prelo authorization for this exact execution, operation and relative path.
// There is deliberately no arbitrary command, environment or shell parameter.
func (p *Podman) RunOperation(ctx context.Context, executionID string, operation Operation, relativePath string, input io.Reader) ([]byte, error) {
	name, err := containerName(executionID)
	if err != nil {
		return nil, err
	}
	switch operation {
	case OperationList, OperationRead, OperationMkdir, OperationCreate:
	default:
		return nil, ErrUnsafeConfig
	}
	if _, err := workspace.ValidatePath(relativePath, operation == OperationList); err != nil {
		return nil, ErrUnsafeConfig
	}
	if operation != OperationCreate && input != nil {
		return nil, ErrUnsafeConfig
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := []string{"exec", "--user=10001:10001"}
	if operation == OperationCreate {
		args = append(args, "--interactive")
	}
	args = append(args, name, "/workspace-helper", string(operation), relativePath)
	output, err := p.run(ctx, input, args...)
	if err != nil {
		return nil, p.classifyResourceFailure(ctx, name, executionID, err)
	}
	return output, nil
}

func (p *Podman) classifyResourceFailure(ctx context.Context, container, executionID string, cause error) error {
	inspectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	state, err := p.run(inspectCtx, nil, "inspect", "--format={{.State.OOMKilled}}", container)
	if err != nil || strings.TrimSpace(string(state)) != "true" {
		return cause
	}
	cleanupErr := p.Stop(context.Background(), executionID)
	limitErr := &ResourceLimitError{Resource: "memory"}
	if cleanupErr != nil {
		return errors.Join(limitErr, fmt.Errorf("container cleanup failed"))
	}
	return limitErr
}

func (p *Podman) run(ctx context.Context, input io.Reader, args ...string) ([]byte, error) {
	if p.storageRoot != "" {
		args = append([]string{"--root=" + p.storageRoot}, args...)
	}
	return p.runner.Run(ctx, input, args...)
}
