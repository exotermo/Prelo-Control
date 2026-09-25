package application

import "github.com/exotermo/hermes-app-go/internal/domain"

// DefaultPermissionPolicy implements ADR-004's rule directly: LOW risk is allowed (once the
// caller has already confirmed the agent holds the capability — this policy is never asked to
// check that itself), MODERATE or HIGH always requires a human's explicit approval. There is
// no per-tenant override yet, and this policy never invents one silently.
type DefaultPermissionPolicy struct{}

func NewDefaultPermissionPolicy() DefaultPermissionPolicy { return DefaultPermissionPolicy{} }

func (DefaultPermissionPolicy) Evaluate(agent domain.AgentDefinition, tool domain.ToolDefinition) domain.PermissionDecision {
	if tool.RiskLevel.RequiresApproval() {
		return domain.DecisionRequireApproval
	}
	return domain.DecisionAllow
}
