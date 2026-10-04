package tools

import (
	"context"
	"strings"

	"github.com/exotermo/prelo-core/internal/domain"
)

// SendWhatsAppTool (Fase T, HIGH): messages a client's WhatsApp contact. HIGH risk means it never
// runs without the owner's explicit approval (DefaultPermissionPolicy), and it refuses clients
// that opted out no matter what was approved.
type SendWhatsAppTool struct {
	clients clientStore
	sender  whatsAppSender
}

type whatsAppSender interface {
	SendWhatsApp(ctx context.Context, to, text string) error
}

const maxWhatsAppText = 1500

func NewSendWhatsAppTool(clients clientStore, sender whatsAppSender) *SendWhatsAppTool {
	return &SendWhatsAppTool{clients: clients, sender: sender}
}

func (*SendWhatsAppTool) Definition() domain.ToolDefinition {
	def, _ := domain.NewToolDefinition("send_whatsapp_message",
		"Envia uma mensagem de WhatsApp para o contato principal de WhatsApp (ou telefone) de um cliente. "+
			"Sempre precisa de aprovação do dono; nunca envia para quem pediu para não receber.", domain.RiskHigh)
	return def.WithSchema(`{"type":"object","properties":{"clientId":{"type":"string"},"text":{"type":"string"}},"required":["clientId","text"],"additionalProperties":false}`,
		"Envia uma mensagem real, em nome da empresa, pelo número de WhatsApp do Prelo — não dá para desfazer depois de enviada.")
}

func (t *SendWhatsAppTool) Execute(ctx context.Context, _ domain.Execution, argsJSON string) (string, error) {
	var args struct {
		ClientID string `json:"clientId"`
		Text     string `json:"text"`
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return "", err
	}
	text := strings.TrimSpace(args.Text)
	if text == "" || len(text) > maxWhatsAppText {
		return "", &domain.ValidationError{Message: "a mensagem precisa ter de 1 a 1500 caracteres"}
	}
	id, err := parseClientID(args.ClientID)
	if err != nil {
		return "", err
	}
	client, err := t.clients.FindByID(ctx, id)
	if err != nil {
		return "", err
	}
	if client.OptedOutAt != nil {
		return "", &domain.ValidationError{Message: "este cliente pediu para não receber mensagens"}
	}
	contacts, err := t.clients.ListContacts(ctx, id)
	if err != nil {
		return "", err
	}
	to := whatsAppTarget(contacts)
	if to == "" {
		return "", &domain.ValidationError{Message: "o cliente não tem WhatsApp nem telefone cadastrado"}
	}
	if err := t.sender.SendWhatsApp(ctx, to, text); err != nil {
		return "", err
	}
	return "mensagem enfileirada para " + client.Name, nil
}

// whatsAppTarget prefers a WhatsApp contact (phone or WhatsApp ID) over a plain phone, primary
// first; phones go as digits ("+5541…" → "5541…"), the form the WhatsApp sidecar addresses.
func whatsAppTarget(contacts []domain.ClientContact) string {
	pick := func(kind domain.ContactKind) string {
		for _, primaryFirst := range []bool{true, false} {
			for _, c := range contacts {
				if c.Kind == kind && c.IsPrimary == primaryFirst {
					return c.Value
				}
			}
		}
		return ""
	}
	value := pick(domain.ContactKindWhatsApp)
	if value == "" {
		value = pick(domain.ContactKindPhone)
	}
	return strings.TrimPrefix(value, "+")
}
