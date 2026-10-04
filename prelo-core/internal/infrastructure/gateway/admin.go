package gateway

import (
	"bytes"
	"context"
	"io"
	"net/http"
)

// AdminClient relays the dashboard's model-connection requests to the gateway's
// /api/v1/admin/connections surface (Fase M). It is a pure pass-through: the request body may
// carry a provider API key on its way to the gateway's vault, and this type never stores, logs
// or inspects it; the response bodies come back already key-free from the gateway.
type AdminClient struct {
	cfg        Config
	httpClient *http.Client
}

func NewAdminClient(cfg Config) *AdminClient {
	return &AdminClient{cfg: cfg, httpClient: NewClient(cfg).httpClient}
}

// Do performs one admin call and returns the gateway's status code and raw JSON body.
func (c *AdminClient) Do(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	token, err := adminToken(c.cfg.JWTSecret, c.cfg.Issuer, c.cfg.Audience)
	if err != nil {
		return 0, nil, &CallError{Status: 502, Message: "Gateway communication failed", Err: err}
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+"/api/v1/admin/connections"+path, reader)
	if err != nil {
		return 0, nil, &CallError{Status: 502, Message: "Gateway communication failed", Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if isTimeout(err) {
			return 0, nil, &CallError{Status: 504, Message: "Gateway did not respond in time", Err: err}
		}
		return 0, nil, &CallError{Status: 502, Message: "Gateway communication failed", Err: err}
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, &CallError{Status: 502, Message: "Gateway communication failed", Err: err}
	}
	return resp.StatusCode, respBody, nil
}
