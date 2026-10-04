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

// NotifyAction (PR-3): an external system asks to do something (e.g. a deploy).
func (n *OwnerApprovalNotifier) NotifyAction(ctx context.Context, view ActionView) {
	n.send(ctx, ActionApprovalMessage(view), view.Approval.ID.String())
}

// NotifyActionResult (PR-3): how the authorized action ended.
func (n *OwnerApprovalNotifier) NotifyActionResult(ctx context.Context, view ActionView) {
	if view.Action.Result == nil {
		return
	}
	n.send(ctx, ActionResultMessage(view), view.Approval.ID.String())
}

func (n *OwnerApprovalNotifier) send(ctx context.Context, text, ref string) {
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := n.notifier.NotifyOwners(sendCtx, text); err != nil {
		log.Printf("owner-approval: could not notify the owner about %s: %v", ref, err)
	}
}

// ActionApprovalMessage is the WhatsApp text for an external action request.
func ActionApprovalMessage(view ActionView) string {
	a := view.Action
	minutes := int(time.Until(view.Approval.ExpiresAt).Round(time.Minute).Minutes())
	if minutes < 1 {
		minutes = 1
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🔐 *Pedido de autorização %s* · risco %s\n\n", view.Approval.ShortCode, riskLabel[a.Risk])
	if a.Kind == domain.ActionKindDeploy {
		d := a.DeployOf()
		fmt.Fprintf(&b, "*Deploy* de %s\nCommit: %s\nAmbiente: %s → %s\nApp: %s\nPedido por: %s\n\n",
			d.Repository, d.CommitSha, d.Environment, d.Target, d.App, a.RequestedBy)
	} else {
		fmt.Fprintf(&b, "%s pedido por %s\n\n", a.Kind, a.RequestedBy)
	}
	fmt.Fprintf(&b, "*Impacto:* %s\n\n", a.Impact)
	fmt.Fprintf(&b, "Aprovar vale só para este commit e este destino. Responda *SIM %s* ou *NÃO %s*. Expira em %d min.",
		view.Approval.ShortCode, view.Approval.ShortCode, minutes)
	return b.String()
}

func ActionResultMessage(view ActionView) string {
	r := view.Action.Result
	icon := map[string]string{"SUCCEEDED": "✅", "FAILED": "❌", "ROLLED_BACK": "↩️", "CANCELLED": "⏹️"}[r.Status]
	label := map[string]string{"SUCCEEDED": "concluído", "FAILED": "falhou", "ROLLED_BACK": "revertido", "CANCELLED": "cancelado"}[r.Status]
	what := view.Action.Kind
	if view.Action.Kind == domain.ActionKindDeploy {
		d := view.Action.DeployOf()
		what = fmt.Sprintf("Deploy de %s@%s em %s", d.Repository, shortSha(d.CommitSha), d.Environment)
	}
	text := fmt.Sprintf("%s %s %s (pedido %s).", icon, what, label, view.Approval.ShortCode)
	if r.URL != nil {
		text += "\n" + *r.URL
	}
	if r.Message != nil {
		text += "\n" + truncateText(*r.Message, 300)
	}
	return text
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
