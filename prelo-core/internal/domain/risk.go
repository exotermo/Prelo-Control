package domain

// RiskLevel classifies a tool for PermissionPolicy (ADR-004). It is set once, on the tool's
// definition, never computed per-call — a tool's risk is a property of what it can do, not of
// the arguments a particular invocation happens to pass.
type RiskLevel string

const (
	RiskLow      RiskLevel = "LOW"
	RiskModerate RiskLevel = "MODERATE"
	RiskHigh     RiskLevel = "HIGH"
)

func (r RiskLevel) Valid() bool {
	switch r {
	case RiskLow, RiskModerate, RiskHigh:
		return true
	default:
		return false
	}
}

// RequiresApproval mirrors ADR-004 literally: "risco moderado ou superior requer confirmação
// explícita salvo política futura igualmente explícita" — no such override policy exists yet,
// so this stays unconditional.
func (r RiskLevel) RequiresApproval() bool {
	return r == RiskModerate || r == RiskHigh
}
