package application

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/exotermo/prelo-core/internal/domain"
)

// OwnerNotifier delivers a text to the owner's WhatsApp (the bridge's owner contacts).
type OwnerNotifier interface {
	NotifyOwners(ctx context.Context, text string) error
}

// OwnerApprovalNotifier (Fase T) tells the owner, on WhatsApp, that an agent is waiting for
// permission: what it wants to do, the risk, the impact in plain words, and how to answer.
// Best effort — the request stays decidable on the dashboard if the message can't be sent.
type OwnerApprovalNotifier struct {
	approvals ApprovalRepository
	calls     ToolCallRepository
	tools     ToolRegistry
	notifier  OwnerNotifier
}

func NewOwnerApprovalNotifier(approvals ApprovalRepository, calls ToolCallRepository, tools ToolRegistry, notifier OwnerNotifier) *OwnerApprovalNotifier {
	return &OwnerApprovalNotifier{approvals: approvals, calls: calls, tools: tools, notifier: notifier}
}

var riskLabel = map[domain.RiskLevel]string{domain.RiskLow: "BAIXO", domain.RiskModerate: "MODERADO", domain.RiskHigh: "ALTO"}

func (n *OwnerApprovalNotifier) Notify(ctx context.Context, approvalID domain.ApprovalRequestID, task domain.Task) {
	approval, err := n.approvals.FindByID(ctx, approvalID)
	if err != nil {
		log.Printf("owner-approval: approval %s not found: %v", approvalID, err)
		return
	}
	call, err := n.calls.FindByID(ctx, approval.ToolCallID)
	if err != nil {
		log.Printf("owner-approval: tool call of %s not found: %v", approvalID, err)
		return
	}
	text := ApprovalMessage(approval, call, task, n.definitionOf(call.ToolName))
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := n.notifier.NotifyOwners(sendCtx, text); err != nil {
		log.Printf("owner-approval: could not notify the owner about %s: %v", approvalID, err)
	}
}

func (n *OwnerApprovalNotifier) definitionOf(name string) domain.ToolDefinition {
	if executor, ok := n.tools.Find(name); ok {
		return executor.Definition()
	}
	return domain.ToolDefinition{Name: name}
}

// ApprovalMessage is the WhatsApp text for one request (exported for tests and the dashboard copy).
func ApprovalMessage(approval domain.ApprovalRequest, call domain.ToolCall, task domain.Task, def domain.ToolDefinition) string {
	risk := riskLabel[call.RiskLevel]
	if risk == "" {
		risk = string(call.RiskLevel)
	}
	impact := def.Impact
	if impact == "" {
		impact = "não descrito"
	}
	minutes := int(time.Until(approval.ExpiresAt).Round(time.Minute).Minutes())
	if minutes < 1 {
		minutes = 1
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🔐 *Pedido de autorização %s* · risco %s\n\n", approval.ShortCode, risk)
	fmt.Fprintf(&b, "Agente *%s* quer usar *%s*", call.AgentID.String(), call.ToolName)
	if args := summarizeArgs(call.ArgsJSON); args != "" {
		fmt.Fprintf(&b, " com: %s", args)
	}
	fmt.Fprintf(&b, "\nTask: %s\n\n", truncateText(task.Description, 160))
	fmt.Fprintf(&b, "*Impacto:* %s\n\n", impact)
	fmt.Fprintf(&b, "Responda *SIM %s* para autorizar ou *NÃO %s* para negar. Expira em %d min.", approval.ShortCode, approval.ShortCode, minutes)
	return b.String()
}

func summarizeArgs(argsJSON string) string {
	trimmed := strings.TrimSpace(argsJSON)
	if trimmed == "" || trimmed == "{}" {
		return ""
	}
	return truncateText(trimmed, 400)
}

func truncateText(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
