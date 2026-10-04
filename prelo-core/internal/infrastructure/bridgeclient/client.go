// Package bridgeclient talks to prelo-messaging-bridge's existing admin API
// (dev.prelo.bridge.web.AdminController) — the same X-Admin-Token header already used for
// pause/resume, no new authentication surface introduced on the bridge for Fase G2.
package bridgeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	adminToken string
	http       *http.Client
}

func New(baseURL, adminToken string) *Client {
	return &Client{baseURL: baseURL, adminToken: adminToken, http: &http.Client{Timeout: 10 * time.Second}}
}

// Configured reports whether the bridge integration has been wired up at all — prelo-core can
// run without it (no owner-contacts management), same "optional integration" pattern as the
// Gateway/Redis wiring in cmd/prelo/main.go.
func (c *Client) Configured() bool { return c.baseURL != "" && c.adminToken != "" }

type ownerContactsBody struct {
	Contacts []string `json:"contacts"`
}

func (c *Client) ListOwnerContacts(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/admin/owner-contacts", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Admin-Token", c.adminToken)
	var body ownerContactsBody
	if err := c.do(req, &body); err != nil {
		return nil, err
	}
	return body.Contacts, nil
}

// ChannelStatus is a WhatsApp channel as reported by messaging-core, proxied through the bridge's
// /admin/channel-status. Fase H4: prelo-dashboard's Integrações page shows this instead of
// requiring a shell/psql session to answer "is WhatsApp connected right now?".
type ChannelStatus struct {
	ID          string `json:"id"`
	ChannelType string `json:"channelType"`
	Status      string `json:"status"`
	ExternalRef string `json:"externalRef"`
	CreatedAt   string `json:"createdAt"`
}

func (c *Client) ChannelStatus(ctx context.Context) ([]ChannelStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/admin/channel-status", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Admin-Token", c.adminToken)
	var channels []ChannelStatus
	if err := c.do(req, &channels); err != nil {
		return nil, err
	}
	return channels, nil
}

type AutoReplyStatus struct {
	StaticEnabled    bool `json:"staticEnabled"`
	RuntimeEnabled   bool `json:"runtimeEnabled"`
	EffectiveEnabled bool `json:"effectiveEnabled"`
}

func (c *Client) GetAutoReplyStatus(ctx context.Context) (*AutoReplyStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/admin/auto-reply", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Admin-Token", c.adminToken)
	var body AutoReplyStatus
	if err := c.do(req, &body); err != nil {
		return nil, err
	}
	return &body, nil
}

func (c *Client) SetAutoReply(ctx context.Context, enabled bool) (*AutoReplyStatus, error) {
	action := "pause"
	if enabled {
		action = "resume"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/admin/"+action, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Admin-Token", c.adminToken)
	var body AutoReplyStatus
	if err := c.do(req, &body); err != nil {
		return nil, err
	}
	return &body, nil
}

func (c *Client) ReplaceOwnerContacts(ctx context.Context, contacts []string) ([]string, error) {
	payload, err := json.Marshal(ownerContactsBody{Contacts: contacts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/admin/owner-contacts", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Admin-Token", c.adminToken)
	req.Header.Set("Content-Type", "application/json")
	var body ownerContactsBody
	if err := c.do(req, &body); err != nil {
		return nil, err
	}
	return body.Contacts, nil
}

// CallError carries both a raw wrapped detail (safe only for server logs) and a fixed
// non-leaking Message — same sanitized-error convention as gateway.CallError, so a bridge
// outage or a rejected phone number never leaks response bodies to the browser.
type CallError struct {
	Status  int
	Message string
	Err     error
}

func (e *CallError) Error() string {
	return fmt.Sprintf("bridge admin call failed (%d): %v", e.Status, e.Err)
}
func (e *CallError) Unwrap() error { return e.Err }

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return &CallError{Message: "could not reach the bridge", Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnprocessableEntity {
		return &CallError{Status: resp.StatusCode, Message: "one of the numbers is not a valid E.164 phone number", Err: fmt.Errorf("bridge returned 422")}
	}
	if resp.StatusCode != http.StatusOK {
		return &CallError{Status: resp.StatusCode, Message: "the bridge rejected the request", Err: fmt.Errorf("unexpected status %d", resp.StatusCode)}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return &CallError{Status: resp.StatusCode, Message: "the bridge returned an unexpected response", Err: err}
	}
	return nil
}
