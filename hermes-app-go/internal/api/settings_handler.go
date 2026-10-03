package api

import (
	"encoding/json"
	"net/http"

	"github.com/exotermo/hermes-app-go/internal/domain"
	"github.com/exotermo/hermes-app-go/internal/infrastructure/bridgeclient"
)

// SettingsHandler is Fase G2's whole surface: managing hermes-messaging-bridge's owner-contacts
// list from hermes-dashboard's Configurações page. It only ever talks to the bridge's existing
// admin API (bridgeclient) — hermes-go itself never stores the phone numbers. Gated to a
// dashboard session by the router (registered as a protected route, and requiredScope maps it
// to settings:manage, which only an ADMIN dashboard session's dashboardAdminScopes grants).
type SettingsHandler struct {
	bridge *bridgeclient.Client
}

func NewSettingsHandler(bridge *bridgeclient.Client) *SettingsHandler {
	return &SettingsHandler{bridge: bridge}
}

func (h *SettingsHandler) GetOwnerContacts(w http.ResponseWriter, r *http.Request) {
	if !h.bridge.Configured() {
		writeError(w, &domain.ValidationError{Message: "the messaging bridge integration is not configured (HERMES_BRIDGE_ADMIN_URL/HERMES_BRIDGE_ADMIN_TOKEN)"})
		return
	}
	contacts, err := h.bridge.ListOwnerContacts(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ownerContactsResponse{Contacts: contacts})
}

func (h *SettingsHandler) PutOwnerContacts(w http.ResponseWriter, r *http.Request) {
	if !h.bridge.Configured() {
		writeError(w, &domain.ValidationError{Message: "the messaging bridge integration is not configured (HERMES_BRIDGE_ADMIN_URL/HERMES_BRIDGE_ADMIN_TOKEN)"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	var req ownerContactsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, &domain.ValidationError{Message: "invalid request body"})
		return
	}
	contacts, err := h.bridge.ReplaceOwnerContacts(r.Context(), req.Contacts)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ownerContactsResponse{Contacts: contacts})
}

type ownerContactsRequest struct {
	Contacts []string `json:"contacts"`
}

type ownerContactsResponse struct {
	Contacts []string `json:"contacts"`
}

// Fase H4: hermes-dashboard's Integrações page — same bridge, same admin token, no new
// dependency. Reuses SettingsHandler rather than a separate handler since both surfaces are
// "proxy this one thing to the bridge's admin API" with identical auth/config requirements.
func (h *SettingsHandler) GetWhatsAppStatus(w http.ResponseWriter, r *http.Request) {
	if !h.bridge.Configured() {
		writeError(w, &domain.ValidationError{Message: "the messaging bridge integration is not configured (HERMES_BRIDGE_ADMIN_URL/HERMES_BRIDGE_ADMIN_TOKEN)"})
		return
	}
	channels, err := h.bridge.ChannelStatus(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, whatsappStatusResponse{Channels: channels})
}

func (h *SettingsHandler) GetAutoReply(w http.ResponseWriter, r *http.Request) {
	if !h.bridge.Configured() {
		writeError(w, &domain.ValidationError{Message: "the messaging bridge integration is not configured (HERMES_BRIDGE_ADMIN_URL/HERMES_BRIDGE_ADMIN_TOKEN)"})
		return
	}
	status, err := h.bridge.GetAutoReplyStatus(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *SettingsHandler) PutAutoReply(w http.ResponseWriter, r *http.Request) {
	if !h.bridge.Configured() {
		writeError(w, &domain.ValidationError{Message: "the messaging bridge integration is not configured (HERMES_BRIDGE_ADMIN_URL/HERMES_BRIDGE_ADMIN_TOKEN)"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	var req autoReplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, &domain.ValidationError{Message: "invalid request body"})
		return
	}
	status, err := h.bridge.SetAutoReply(r.Context(), req.Enabled)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

type whatsappStatusResponse struct {
	Channels []bridgeclient.ChannelStatus `json:"channels"`
}

type autoReplyRequest struct {
	Enabled bool `json:"enabled"`
}
