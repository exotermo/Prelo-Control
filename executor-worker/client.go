package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type heartbeatResponse struct {
	WorkerID   string `json:"workerId"`
	ProjectID  string `json:"projectId"`
	CanExecute bool   `json:"canExecute"`
	Status     string `json:"status"`
}

type preloClient struct {
	base     string
	endpoint string
	token    string
	http     *http.Client
}

var errRevoked = errors.New("worker credential rejected")

func newPreloClient(baseURL, token string, allowLocalHTTP bool) (*preloClient, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && !(allowLocalHTTP && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		return nil, fmt.Errorf("PRELO_BASE_URL must be an HTTPS origin")
	}
	if !strings.HasPrefix(token, "prw_") || len(token) != 47 {
		return nil, fmt.Errorf("PRELO_EXECUTOR_TOKEN is invalid")
	}
	base := strings.TrimRight(u.String(), "/")
	return &preloClient{base: base, endpoint: base + "/api/v1/executor-workers/heartbeat", token: token,
		http: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *preloClient) heartbeat(ctx context.Context) (heartbeatResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, nil)
	if err != nil {
		return heartbeatResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return heartbeatResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return heartbeatResponse{}, errRevoked
	}
	if resp.StatusCode != http.StatusOK {
		return heartbeatResponse{}, fmt.Errorf("heartbeat returned HTTP %d", resp.StatusCode)
	}
	var result heartbeatResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024)).Decode(&result); err != nil {
		return heartbeatResponse{}, err
	}
	if result.WorkerID == "" || result.ProjectID == "" || (result.CanExecute && result.Status != "READY") || (!result.CanExecute && result.Status != "REGISTERED_NO_RUNTIME") {
		return heartbeatResponse{}, fmt.Errorf("unexpected heartbeat response")
	}
	return result, nil
}
