package bridgeclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListOwnerContacts_SendsTheAdminTokenAndParsesTheList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/owner-contacts" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Admin-Token"); got != "secret" {
			t.Fatalf("expected the admin token to be forwarded, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(ownerContactsBody{Contacts: []string{"+5541984450529"}})
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	if !client.Configured() {
		t.Fatal("expected the client to report itself configured")
	}
	contacts, err := client.ListOwnerContacts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(contacts) != 1 || contacts[0] != "+5541984450529" {
		t.Fatalf("unexpected contacts: %+v", contacts)
	}
}

func TestReplaceOwnerContacts_SendsThePUTBodyAndReturnsTheNewList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("expected PUT, got %s", r.Method)
		}
		var body ownerContactsBody
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Contacts) != 2 {
			t.Fatalf("expected 2 contacts in the request body, got %+v", body.Contacts)
		}
		_ = json.NewEncoder(w).Encode(ownerContactsBody{Contacts: body.Contacts})
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	contacts, err := client.ReplaceOwnerContacts(context.Background(), []string{"+5541984450529", "+5541996635461"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(contacts) != 2 {
		t.Fatalf("unexpected contacts: %+v", contacts)
	}
}

func TestReplaceOwnerContacts_422IsMappedToANonLeakingCallError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	_, err := client.ReplaceOwnerContacts(context.Background(), []string{"not-e164"})
	var callErr *CallError
	if err == nil {
		t.Fatal("expected an error")
	}
	if ce, ok := err.(*CallError); ok {
		callErr = ce
	} else {
		t.Fatalf("expected a *CallError, got %T", err)
	}
	if callErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d", callErr.Status)
	}
}

func TestUnconfiguredClient_ReportsItself(t *testing.T) {
	if (&Client{}).Configured() {
		t.Fatal("an empty client must report itself as not configured")
	}
}
