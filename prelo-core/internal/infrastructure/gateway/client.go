package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

const (
	connectTimeout = 5 * time.Second
	// Real models (Fase M connections) can take tens of seconds to answer; the gateway itself
	// caps a connection call at 60s, so this only needs to outlast that.
	requestTimeout = 75 * time.Second
)

type Config struct {
	BaseURL   string
	JWTSecret string
	Issuer    string
	Audience  string

	// RequestTimeout/ConnectTimeout override the production defaults (10s/5s, matching the
	// Java client's REQUEST_TIMEOUT/CONNECT_TIMEOUT); left zero in production, only set by
	// tests that need to exercise the timeout path quickly.
	RequestTimeout time.Duration
	ConnectTimeout time.Duration
}

type Client struct {
	cfg        Config
	httpClient *http.Client
}

func NewClient(cfg Config) *Client {
	reqTimeout := requestTimeout
	if cfg.RequestTimeout > 0 {
		reqTimeout = cfg.RequestTimeout
	}
	connTimeout := connectTimeout
	if cfg.ConnectTimeout > 0 {
		connTimeout = cfg.ConnectTimeout
	}
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: reqTimeout,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{Timeout: connTimeout}).DialContext,
			},
		},
	}
}

// Chat mirrors GatewayClient.chat(): POST {baseUrl}/api/v1/llm/chat with a fresh
// service-to-service JWT, mapping errors into the same three buckets Java uses — non-200 ->
// CallError carrying the upstream status ("Gateway rejected LLM request"), timeout -> 504
// ("Gateway did not respond in time"), any other transport failure -> 502 ("Gateway
// communication failed").
func (c *Client) Chat(ctx context.Context, req ChatRequest, requestID string) (ChatResponse, error) {
	token, err := serviceToken(c.cfg.JWTSecret, c.cfg.Issuer, c.cfg.Audience)
	if err != nil {
		return ChatResponse{}, &CallError{Status: 502, Message: "Gateway communication failed", Err: err}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return ChatResponse{}, &CallError{Status: 502, Message: "Gateway communication failed", Err: err}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/api/v1/llm/chat", bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, &CallError{Status: 502, Message: "Gateway communication failed", Err: err}
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Request-Id", requestID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if isTimeout(err) {
			return ChatResponse{}, &CallError{Status: 504, Message: "Gateway did not respond in time", Err: err}
		}
		return ChatResponse{}, &CallError{Status: 502, Message: "Gateway communication failed", Err: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResponse{}, &CallError{Status: 502, Message: "Gateway communication failed", Err: err}
	}

	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, &CallError{Status: resp.StatusCode, Message: "Gateway rejected LLM request"}
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return ChatResponse{}, &CallError{Status: 502, Message: "Gateway communication failed", Err: err}
	}
	return chatResp, nil
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}
