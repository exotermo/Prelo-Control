package domain

import "testing"

func TestNewToolDefinitionValid(t *testing.T) {
	def, err := NewToolDefinition("current_time", "Returns the current server time.", RiskLow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.Name != "current_time" || def.RiskLevel != RiskLow {
		t.Fatalf("unexpected definition: %+v", def)
	}
}

func TestNewToolDefinitionRejectsBlankName(t *testing.T) {
	if _, err := NewToolDefinition("", "desc", RiskLow); err == nil {
		t.Fatal("expected an error for a blank name")
	}
}

func TestNewToolDefinitionRejectsInvalidRisk(t *testing.T) {
	if _, err := NewToolDefinition("tool", "desc", RiskLevel("NUCLEAR")); err == nil {
		t.Fatal("expected an error for an invalid risk level")
	}
}

func TestRiskLevelRequiresApproval(t *testing.T) {
	if RiskLow.RequiresApproval() {
		t.Fatal("LOW should not require approval")
	}
	if !RiskModerate.RequiresApproval() {
		t.Fatal("MODERATE should require approval")
	}
	if !RiskHigh.RequiresApproval() {
		t.Fatal("HIGH should require approval")
	}
}
