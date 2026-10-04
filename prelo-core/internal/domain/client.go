package domain

import (
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Fase C1: a Client is the customer a set of projects (and WhatsApp conversations, Fase C2)
// belongs to. Leads found by prospecting (Fase L1) are Clients too, with Status LEAD.

type ClientID struct{ Value uuid.UUID }

func NewClientID() ClientID { return ClientID{Value: uuid.New()} }

func (id ClientID) String() string { return id.Value.String() }

type ClientStatus string

const (
	ClientStatusLead      ClientStatus = "LEAD"
	ClientStatusActive    ClientStatus = "ACTIVE"
	ClientStatusInactive  ClientStatus = "INACTIVE"
	ClientStatusDiscarded ClientStatus = "DISCARDED"
)

var validClientStatuses = map[ClientStatus]bool{ClientStatusLead: true, ClientStatusActive: true, ClientStatusInactive: true, ClientStatusDiscarded: true}

// ClientStage is the prospecting funnel position — only meaningful for leads (Fase L1/L2).
type ClientStage string

const (
	ClientStageNew       ClientStage = "NEW"
	ClientStageAnalyzed  ClientStage = "ANALYZED"
	ClientStageContacted ClientStage = "CONTACTED"
	ClientStageReplied   ClientStage = "REPLIED"
	ClientStageQualified ClientStage = "QUALIFIED"
)

var validClientStages = map[ClientStage]bool{ClientStageNew: true, ClientStageAnalyzed: true, ClientStageContacted: true, ClientStageReplied: true, ClientStageQualified: true}

type ClientSource string

const (
	ClientSourceManual   ClientSource = "MANUAL"
	ClientSourceOSM      ClientSource = "OSM"
	ClientSourceWhatsApp ClientSource = "WHATSAPP"
)

const (
	MaxClientNameLength  = 200
	MaxClientNotesLength = 10000
)

type Client struct {
	ID          ClientID
	Name        string
	Company     *string
	Status      ClientStatus
	Stage       ClientStage
	Source      ClientSource
	Address     *string
	City        *string
	Website     *string
	ExternalRef *string
	Notes       *string
	OptedOutAt  *time.Time
	CreatedAt   time.Time
	CreatedBy   string
	UpdatedAt   time.Time
	Version     int64
}

// ClientFields are the human-editable fields, validated together by NewClient and WithFields.
type ClientFields struct {
	Name    string
	Company *string
	Status  ClientStatus
	Stage   ClientStage
	Address *string
	City    *string
	Website *string
	Notes   *string
}

func (f ClientFields) validate() error {
	if isBlank(f.Name) || len(f.Name) > MaxClientNameLength {
		return &ValidationError{Message: "client name is required and must be at most 200 characters"}
	}
	if !validClientStatuses[f.Status] {
		return &ValidationError{Message: "unknown client status"}
	}
	if !validClientStages[f.Stage] {
		return &ValidationError{Message: "unknown client stage"}
	}
	if f.Notes != nil && len(*f.Notes) > MaxClientNotesLength {
		return &ValidationError{Message: "notes must be at most 10000 characters"}
	}
	if f.Website != nil && !isBlank(*f.Website) {
		w := strings.ToLower(strings.TrimSpace(*f.Website))
		if !strings.HasPrefix(w, "http://") && !strings.HasPrefix(w, "https://") {
			return &ValidationError{Message: "website must start with http:// or https://"}
		}
	}
	return nil
}

func NewClient(fields ClientFields, source ClientSource, createdBy string) (Client, error) {
	if fields.Status == "" {
		fields.Status = ClientStatusActive
	}
	if fields.Stage == "" {
		fields.Stage = ClientStageNew
	}
	if err := fields.validate(); err != nil {
		return Client{}, err
	}
	now := time.Now().UTC()
	c := Client{ID: NewClientID(), Source: source, CreatedAt: now, CreatedBy: createdBy, UpdatedAt: now}
	return c.apply(fields), nil
}

func (c Client) WithFields(fields ClientFields) (Client, error) {
	if err := fields.validate(); err != nil {
		return Client{}, err
	}
	return c.apply(fields), nil
}

func (c Client) apply(f ClientFields) Client {
	c.Name = strings.TrimSpace(f.Name)
	c.Company = blankToNil(f.Company)
	c.Status = f.Status
	c.Stage = f.Stage
	c.Address = blankToNil(f.Address)
	c.City = blankToNil(f.City)
	c.Website = blankToNil(f.Website)
	c.Notes = blankToNil(f.Notes)
	return c
}

type ContactKind string

const (
	ContactKindPhone    ContactKind = "PHONE"
	ContactKindWhatsApp ContactKind = "WHATSAPP"
	ContactKindEmail    ContactKind = "EMAIL"
)

type ClientContact struct {
	ID        uuid.UUID
	ClientID  ClientID
	Kind      ContactKind
	Value     string
	IsPrimary bool
	CreatedAt time.Time
}

// NewClientContact normalizes the value (phones to E.164, e-mails lowercased) so the same
// number typed two ways is still one contact — Fase C2 matches WhatsApp senders on it.
func NewClientContact(clientID ClientID, kind ContactKind, raw string, isPrimary bool) (ClientContact, error) {
	var value string
	switch kind {
	case ContactKindPhone, ContactKindWhatsApp:
		phone, err := NormalizePhone(raw)
		if err != nil {
			return ClientContact{}, err
		}
		value = phone
	case ContactKindEmail:
		addr, err := mail.ParseAddress(strings.TrimSpace(raw))
		if err != nil || addr.Name != "" {
			return ClientContact{}, &ValidationError{Message: "invalid e-mail address"}
		}
		value = strings.ToLower(addr.Address)
	default:
		return ClientContact{}, &ValidationError{Message: "contact kind must be PHONE, WHATSAPP or EMAIL"}
	}
	return ClientContact{ID: uuid.New(), ClientID: clientID, Kind: kind, Value: value, IsPrimary: isPrimary, CreatedAt: time.Now().UTC()}, nil
}

var e164 = regexp.MustCompile(`^\+[1-9]\d{1,14}$`)

// NormalizePhone turns what a person types into E.164 (same rule as the bridge's
// owner-contacts). Without a country code, 10–11 digit numbers are taken as Brazilian
// (DDD + number), the only market this instance serves today.
func NormalizePhone(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	international := strings.HasPrefix(trimmed, "+") || strings.HasPrefix(trimmed, "00")
	var digits strings.Builder
	for _, r := range trimmed {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	switch {
	case strings.HasPrefix(trimmed, "00"):
		d = strings.TrimPrefix(d, "00")
	case international:
	case (len(d) == 11 || len(d) == 12) && strings.HasPrefix(d, "0"):
		d = "55" + strings.TrimLeft(d, "0")
	case len(d) == 10 || len(d) == 11:
		d = "55" + d
	case (len(d) == 12 || len(d) == 13) && strings.HasPrefix(d, "55"):
	default:
		return "", &ValidationError{Message: "invalid phone number: use +<country><number>, or a Brazilian DDD + number"}
	}
	phone := "+" + d
	if !e164.MatchString(phone) {
		return "", &ValidationError{Message: "invalid phone number"}
	}
	return phone, nil
}
