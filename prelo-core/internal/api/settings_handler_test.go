package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/exotermo/prelo-core/internal/infrastructure/bridgeclient"
)

func TestSettingsHandler_GetOwnerContacts_Unconfigured_ReturnsAClearError(t *testing.T) {
	handler := NewSettingsHandler(bridgeclient.New("", ""))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/owner-contacts", nil)
	rec := httptest.NewRecorder()
	handler.GetOwnerContacts(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSettingsHandler_GetAndPutOwnerContacts_ProxyToTheBridge(t *testing.T) {
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"contacts": []string{"+5541984450529"}})
		case http.MethodPut:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{"contacts": body["contacts"]})
		}
	}))
	defer bridge.Close()

	handler := NewSettingsHandler(bridgeclient.New(bridge.URL, "token"))

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/settings/owner-contacts", nil)
	getRec := httptest.NewRecorder()
	handler.GetOwnerContacts(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}
	var getResp ownerContactsResponse
	_ = json.Unmarshal(getRec.Body.Bytes(), &getResp)
	if len(getResp.Contacts) != 1 || getResp.Contacts[0] != "+5541984450529" {
		t.Fatalf("unexpected GET response: %+v", getResp)
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/v1/settings/owner-contacts", strings.NewReader(`{"contacts":["+5541984450529","+5541996635461"]}`))
	putRec := httptest.NewRecorder()
	handler.PutOwnerContacts(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", putRec.Code, putRec.Body.String())
	}
	var putResp ownerContactsResponse
	_ = json.Unmarshal(putRec.Body.Bytes(), &putResp)
	if len(putResp.Contacts) != 2 {
		t.Fatalf("unexpected PUT response: %+v", putResp)
	}
}

func TestSettingsHandler_GetWhatsAppStatus_ProxiesTheBridgesChannelList(t *testing.T) {
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/channel-status" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "chan-1", "channelType": "WHATSAPP", "status": "CONNECTED", "externalRef": "554184450529:9@s.whatsapp.net", "createdAt": "2026-09-29T18:43:27Z"},
		})
	}))
	defer bridge.Close()

	handler := NewSettingsHandler(bridgeclient.New(bridge.URL, "token"))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/integrations/whatsapp", nil)
	rec := httptest.NewRecorder()
	handler.GetWhatsAppStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp whatsappStatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Channels) != 1 || resp.Channels[0].Status != "CONNECTED" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestSettingsHandler_PutAutoReply_ProxiesToPauseOrResume(t *testing.T) {
	var calledPath string
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"staticEnabled": true, "runtimeEnabled": true, "effectiveEnabled": true})
	}))
	defer bridge.Close()

	handler := NewSettingsHandler(bridgeclient.New(bridge.URL, "token"))
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/integrations/whatsapp/auto-reply", strings.NewReader(`{"enabled":true}`))
	rec := httptest.NewRecorder()
	handler.PutAutoReply(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if calledPath != "/admin/resume" {
		t.Fatalf("expected bridge call to /admin/resume, got %s", calledPath)
	}
}
