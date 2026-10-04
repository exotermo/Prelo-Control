package api

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

// IntegrationHandler is Fase I's surface: per-project API keys and outbound webhooks. Every
// route is integrations:manage (ADMIN) via requiredScope and project-scoped via X-Project-Id —
// a project is mandatory here, there is no "unassigned bucket" for integrations.
type IntegrationHandler struct {
	service *application.IntegrationService
}

func NewIntegrationHandler(service *application.IntegrationService) *IntegrationHandler {
	return &IntegrationHandler{service: service}
}

type createApiKeyRequest struct {
	Name          string   `json:"name"`
	Scopes        []string `json:"scopes"`
	ExpiresInDays int      `json:"expiresInDays"`
}

type createWebhookRequest struct {
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Events []string `json:"events"`
}

type apiKeyResponse struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	DisplayPrefix string   `json:"displayPrefix"`
	Scopes        []string `json:"scopes"`
	ExpiresAt     *string  `json:"expiresAt"`
	LastUsedAt    *string  `json:"lastUsedAt"`
	CreatedAt     string   `json:"createdAt"`
}

type deliveryResponse struct {
	Event      string  `json:"event"`
	Status     string  `json:"status"`
	StatusCode *int    `json:"statusCode"`
	Error      *string `json:"error"`
	Attempt    int     `json:"attempt"`
	CreatedAt  string  `json:"createdAt"`
}

type webhookResponse struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	URL          string            `json:"url"`
	Events       []string          `json:"events"`
	CreatedAt    string            `json:"createdAt"`
	LastDelivery *deliveryResponse `json:"lastDelivery"`
}

type integrationsResponse struct {
	ApiKeys  []apiKeyResponse  `json:"apiKeys"`
	Webhooks []webhookResponse `json:"webhooks"`
}

func formatTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

func apiKeyResponseFrom(k domain.ApiKey) apiKeyResponse {
	return apiKeyResponse{ID: k.ID.String(), Name: k.Name, DisplayPrefix: k.DisplayPrefix, Scopes: k.Scopes,
		ExpiresAt: formatTime(k.ExpiresAt), LastUsedAt: formatTime(k.LastUsedAt), CreatedAt: k.CreatedAt.Format(time.RFC3339)}
}

func webhookResponseFrom(w domain.Webhook, last *domain.WebhookDelivery) webhookResponse {
	resp := webhookResponse{ID: w.ID.String(), Name: w.Name, URL: w.URL, Events: w.Events, CreatedAt: w.CreatedAt.Format(time.RFC3339)}
	if last != nil {
		resp.LastDelivery = &deliveryResponse{Event: last.Event, Status: string(last.Status), StatusCode: last.LastStatusCode,
			Error: last.LastError, Attempt: last.Attempt, CreatedAt: last.CreatedAt.Format(time.RFC3339)}
	}
	return resp
}

func requireProject(w http.ResponseWriter, r *http.Request) (domain.ProjectID, bool) {
	raw := projectIdentity(r.Context())
	if raw == nil {
		writeError(w, &domain.ValidationError{Message: "select a project (X-Project-Id) to manage its integrations"})
		return domain.ProjectID{}, false
	}
	return domain.ProjectID{Value: *raw}, true
}

func callerSubject(r *http.Request) string {
	if identity, ok := FromContext(r.Context()); ok {
		return identity.Subject
	}
	return ""
}

func parsePathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		writeError(w, &domain.ValidationError{Message: name + " must be a valid UUID"})
		return uuid.UUID{}, false
	}
	return id, true
}

func (h *IntegrationHandler) List(w http.ResponseWriter, r *http.Request) {
	projectID, ok := requireProject(w, r)
	if !ok {
		return
	}
	keys, webhooks, err := h.service.List(r.Context(), projectID)
	if err != nil {
		writeError(w, err)
		return
	}
	resp := integrationsResponse{ApiKeys: make([]apiKeyResponse, 0, len(keys)), Webhooks: make([]webhookResponse, 0, len(webhooks))}
	for _, k := range keys {
		resp.ApiKeys = append(resp.ApiKeys, apiKeyResponseFrom(k))
	}
	for _, wh := range webhooks {
		resp.Webhooks = append(resp.Webhooks, webhookResponseFrom(wh.Webhook, wh.LastDelivery))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *IntegrationHandler) CreateApiKey(w http.ResponseWriter, r *http.Request) {
	projectID, ok := requireProject(w, r)
	if !ok {
		return
	}
	var req createApiKeyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	key, raw, err := h.service.CreateApiKey(r.Context(), projectID, req.Name, req.Scopes, req.ExpiresInDays, callerSubject(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"apiKey": apiKeyResponseFrom(key), "rawKey": raw})
}

func (h *IntegrationHandler) RevokeApiKey(w http.ResponseWriter, r *http.Request) {
	projectID, ok := requireProject(w, r)
	if !ok {
		return
	}
	id, ok := parsePathUUID(w, r, "keyId")
	if !ok {
		return
	}
	if err := h.service.RevokeApiKey(r.Context(), projectID, domain.ApiKeyID{Value: id}); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *IntegrationHandler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	projectID, ok := requireProject(w, r)
	if !ok {
		return
	}
	var req createWebhookRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	webhook, secret, err := h.service.CreateWebhook(r.Context(), projectID, req.Name, req.URL, req.Events, callerSubject(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"webhook": webhookResponseFrom(webhook, nil), "secret": secret})
}

func (h *IntegrationHandler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	projectID, ok := requireProject(w, r)
	if !ok {
		return
	}
	id, ok := parsePathUUID(w, r, "webhookId")
	if !ok {
		return
	}
	if err := h.service.DeleteWebhook(r.Context(), projectID, domain.WebhookID{Value: id}); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *IntegrationHandler) TestWebhook(w http.ResponseWriter, r *http.Request) {
	projectID, ok := requireProject(w, r)
	if !ok {
		return
	}
	id, ok := parsePathUUID(w, r, "webhookId")
	if !ok {
		return
	}
	if err := h.service.SendTest(r.Context(), projectID, domain.WebhookID{Value: id}); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
