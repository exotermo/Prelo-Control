package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/exotermo/prelo-executor-worker/internal/dispatch"
	containerruntime "github.com/exotermo/prelo-executor-worker/internal/runtime"
)

type jobAuthority struct {
	client  *preloClient
	claimed executorJob
}

func (a jobAuthority) Current(ctx context.Context, jobID string) (dispatch.Grant, error) {
	current, err := a.client.current(ctx, jobID)
	if err != nil {
		return dispatch.Grant{}, err
	}
	if current.JobID != a.claimed.JobID || current.LeaseID != a.claimed.LeaseID || (current.Status != "CLAIMED" && current.Status != "RUNNING") || current.Request.PayloadHash != a.claimed.Request.PayloadHash || current.Request.ID != a.claimed.Request.ID {
		return dispatch.Grant{}, dispatch.ErrUnauthorized
	}
	p := current.Request.Payload
	return dispatch.Grant{JobID: current.JobID, ProjectID: p.ProjectID, WorkerID: p.WorkerID, ExecutionID: p.ExecutionID,
		ImageDigest: p.ImageDigest, PayloadHash: current.Request.PayloadHash, Status: current.Request.Status, Operation: p.Operation, StartBefore: current.Request.StartBefore}, nil
}

func validateJobHash(job executorJob) error {
	canonical, err := json.Marshal(job.Request.Payload)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(canonical)
	if job.Request.PayloadHash != "sha256:"+hex.EncodeToString(digest[:]) {
		return dispatch.ErrUnauthorized
	}
	return nil
}

// runClaimedJob is the only path from a claimed job to Podman. It asks Prelo again before
// reporting RUNNING; any transport, lease, payload or approval mismatch returns without side
// effects. A failure after RUNNING is reported as FAILED without leaking file contents.
func runClaimedJob(ctx context.Context, c *preloClient, podman *containerruntime.Podman, workerID, projectID, imageDigest string, job executorJob) error {
	if err := validateJobHash(job); err != nil {
		return err
	}
	payload := job.Request.Payload
	assignment := dispatch.Assignment{JobID: job.JobID, ProjectID: payload.ProjectID, WorkerID: payload.WorkerID,
		ExecutionID: payload.ExecutionID, ImageDigest: payload.ImageDigest, PayloadHash: job.Request.PayloadHash, Operation: payload.Operation}
	starter := dispatch.NewStarter(jobAuthority{client: c, claimed: job}, podman, workerID, projectID, imageDigest)
	if err := starter.Verify(ctx, assignment); err != nil {
		return err
	}
	if err := c.result(ctx, job.JobID, job.LeaseID, 1, "RUNNING", json.RawMessage(`{}`)); err != nil {
		return err
	}
	if err := starter.Verify(ctx, assignment); err != nil {
		_ = c.result(ctx, job.JobID, job.LeaseID, 2, "FAILED", json.RawMessage(`{"code":"authorization_lost"}`))
		return err
	}
	var output json.RawMessage
	var err error
	switch payload.Operation {
	case "START_WORKSPACE":
		// The second GET above closes the gap before Podman starts.
		err = podman.Start(ctx, payload.ExecutionID)
		output = json.RawMessage(`{"started":true}`)
	case "LIST", "READ", "MKDIR", "CREATE":
		var input io.Reader
		if payload.Operation == "CREATE" {
			content, decodeErr := base64.StdEncoding.DecodeString(payload.Args.ContentBase64)
			if decodeErr != nil || len(content) > 64<<10 {
				err = dispatch.ErrUnauthorized
				break
			}
			input = strings.NewReader(string(content))
		}
		op := map[string]containerruntime.Operation{"LIST": containerruntime.OperationList, "READ": containerruntime.OperationRead, "MKDIR": containerruntime.OperationMkdir, "CREATE": containerruntime.OperationCreate}[payload.Operation]
		var data []byte
		data, err = podman.RunOperation(ctx, payload.ExecutionID, op, payload.Args.Path, input)
		if err == nil {
			output = json.RawMessage(data)
			if !json.Valid(output) {
				err = errors.New("invalid helper response")
			}
		}
		if err == nil && payload.Operation == "CREATE" {
			content, _ := base64.StdEncoding.DecodeString(payload.Args.ContentBase64)
			if job.FileID == "" {
				err = dispatch.ErrUnauthorized
			} else {
				err = c.upload(ctx, job.JobID, job.FileID, job.LeaseID, content)
				if err == nil {
					var written struct {
						Size   int64  `json:"size"`
						SHA256 string `json:"sha256"`
					}
					if json.Unmarshal(data, &written) != nil {
						err = errors.New("invalid helper create response")
					} else {
						output, err = json.Marshal(map[string]any{"published": true, "fileId": job.FileID, "path": payload.Args.Path, "size": written.Size, "sha256": written.SHA256})
					}
				}
			}
		}
	default:
		err = dispatch.ErrUnauthorized
	}
	if err != nil {
		failure := json.RawMessage(`{"code":"executor_failed"}`)
		var limitErr *containerruntime.ResourceLimitError
		if errors.As(err, &limitErr) {
			failure, _ = json.Marshal(map[string]string{"code": "resource_limit_exceeded", "resource": limitErr.Resource})
		}
		_ = c.result(ctx, job.JobID, job.LeaseID, 2, "FAILED", failure)
		return fmt.Errorf("executor job failed: %w", err)
	}
	if len(output) > 64<<10 {
		output = json.RawMessage(`{"code":"output_limit"}`)
	}
	if err := c.result(ctx, job.JobID, job.LeaseID, 2, "SUCCEEDED", output); err != nil {
		return err
	}
	return nil
}

func digestFromImage(image string) string {
	if i := strings.LastIndex(image, "@sha256:"); i >= 0 {
		return image[i+1:]
	}
	return ""
}
