package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type executorPayload struct {
	ProjectID   string `json:"projectId"`
	TaskID      string `json:"taskId"`
	ExecutionID string `json:"executionId"`
	WorkerID    string `json:"workerId"`
	ImageDigest string `json:"imageDigest"`
	Operation   string `json:"operation"`
	Args        struct {
		Path          string `json:"path,omitempty"`
		ContentBase64 string `json:"contentBase64,omitempty"`
	} `json:"args"`
}
type executorRequest struct {
	ID          string          `json:"id"`
	Payload     executorPayload `json:"payload"`
	PayloadHash string          `json:"payloadHash"`
	Status      string          `json:"status"`
	StartBefore time.Time       `json:"startBefore"`
}
type executorJob struct {
	JobID      string          `json:"jobId"`
	Request    executorRequest `json:"request"`
	Status     string          `json:"status"`
	LeaseID    string          `json:"leaseId"`
	LeaseUntil time.Time       `json:"leaseUntil"`
	Sequence   int             `json:"sequence"`
	FileID     string          `json:"fileId"`
}

type executorClaimCapacity struct {
	ProfileID          string    `json:"profileId"`
	MemoryBytes        int64     `json:"memoryBytes"`
	CPUQuotaMilli      int64     `json:"cpuQuotaMilli"`
	DiskBytes          int64     `json:"diskBytes"`
	PIDs               int64     `json:"pids"`
	AvailableSlots     int       `json:"availableSlots"`
	MaximumSlots       int       `json:"maximumSlots"`
	ActiveContainers   int       `json:"activeContainers"`
	ActiveExecutionIDs []string  `json:"activeExecutionIds"`
	ObservedAt         time.Time `json:"observedAt"`
}

func (c *preloClient) jobRequest(ctx context.Context, method, path string, body io.Reader, headers http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	return c.http.Do(req)
}
func decodeJobResponse(resp *http.Response) (executorJob, error) {
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return executorJob{}, errRevoked
	}
	if resp.StatusCode != http.StatusOK {
		return executorJob{}, fmt.Errorf("executor API returned HTTP %d", resp.StatusCode)
	}
	var job executorJob
	if err := json.NewDecoder(io.LimitReader(resp.Body, 128<<10)).Decode(&job); err != nil {
		return executorJob{}, err
	}
	if job.JobID == "" || job.LeaseID == "" || job.Request.ID == "" || job.Request.PayloadHash == "" {
		return executorJob{}, errors.New("invalid executor job")
	}
	return job, nil
}
func (c *preloClient) claim(ctx context.Context, capacity executorClaimCapacity) (*executorJob, error) {
	body, err := json.Marshal(struct {
		Capacity executorClaimCapacity `json:"capacity"`
	}{Capacity: capacity})
	if err != nil {
		return nil, err
	}
	headers := http.Header{"Content-Type": []string{"application/json"}}
	resp, err := c.jobRequest(ctx, http.MethodPost, "/api/v1/executor-workers/jobs/claim", bytes.NewReader(body), headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNoContent {
		resp.Body.Close()
		return nil, nil
	}
	job, err := decodeJobResponse(resp)
	if err != nil {
		return nil, err
	}
	return &job, nil
}
func (c *preloClient) current(ctx context.Context, jobID string) (executorJob, error) {
	resp, err := c.jobRequest(ctx, http.MethodGet, "/api/v1/executor-workers/jobs/"+url.PathEscape(jobID), nil, nil)
	if err != nil {
		return executorJob{}, err
	}
	return decodeJobResponse(resp)
}
func (c *preloClient) result(ctx context.Context, jobID, leaseID string, sequence int, status string, result json.RawMessage) error {
	body, err := json.Marshal(struct {
		LeaseID  string          `json:"leaseId"`
		Sequence int             `json:"sequence"`
		Status   string          `json:"status"`
		Result   json.RawMessage `json:"result"`
	}{leaseID, sequence, status, result})
	if err != nil {
		return err
	}
	headers := http.Header{"Content-Type": []string{"application/json"}}
	resp, err := c.jobRequest(ctx, http.MethodPost, "/api/v1/executor-workers/jobs/"+url.PathEscape(jobID)+"/result", bytes.NewReader(body), headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errRevoked
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("result returned HTTP %d", resp.StatusCode)
	}
	return nil
}
func (c *preloClient) upload(ctx context.Context, jobID, fileID, leaseID string, content []byte) error {
	headers := http.Header{"X-Executor-Lease-Id": []string{leaseID}, "Content-Type": []string{"application/octet-stream"}}
	resp, err := c.jobRequest(ctx, http.MethodPut, "/api/v1/executor-workers/jobs/"+url.PathEscape(jobID)+"/files/"+url.PathEscape(fileID), bytes.NewReader(content), headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errRevoked
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("file publish returned HTTP %d", resp.StatusCode)
	}
	return nil
}
