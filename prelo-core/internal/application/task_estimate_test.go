package application

import (
	"context"
	"testing"

	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/google/uuid"
)

type estimateAgents struct{ agent domain.AgentDefinition }

func (f estimateAgents) Find(id domain.AgentID) (domain.AgentDefinition, bool) {
	return f.agent, id == f.agent.AgentID
}
func (f estimateAgents) FindRequired(id domain.AgentID) (domain.AgentDefinition, error) {
	if agent, ok := f.Find(id); ok {
		return agent, nil
	}
	return domain.AgentDefinition{}, &domain.ErrUnknownAgent{AgentID: id.String()}
}

type estimateProjects struct{ project domain.Project }

func (f estimateProjects) FindByID(context.Context, domain.ProjectID) (domain.Project, error) {
	return f.project, nil
}

type estimateSamples struct {
	samples  []TaskEstimateSample
	capacity *TaskEstimateCapacity
}

func (f estimateSamples) Samples(context.Context, string, string, string, int) ([]TaskEstimateSample, error) {
	return f.samples, nil
}
func (f estimateSamples) Capacity(context.Context, uuid.UUID) (*TaskEstimateCapacity, error) {
	return f.capacity, nil
}

func TestTaskEstimatorSeparatesHistoricalTokenAndDurationRanges(t *testing.T) {
	agentID, _ := domain.NewAgentID("general")
	agent, _ := domain.NewAgentDefinition(agentID, domain.AgentTypeGeneral, "1", "helpful", "codex-default", nil, "General", "")
	repo := estimateSamples{samples: []TaskEstimateSample{
		{InputTokens: 100, OutputTokens: 20, DurationMs: 1000},
		{InputTokens: 200, OutputTokens: 40, DurationMs: 3000},
		{InputTokens: 300, OutputTokens: 60, DurationMs: 5000},
		{InputTokens: 400, OutputTokens: 80, DurationMs: 7000},
		{InputTokens: 500, OutputTokens: 100, DurationMs: 9000},
	}}
	estimator := NewTaskEstimator(estimateAgents{agent: agent}, nil, repo)
	estimate, err := estimator.Estimate(context.Background(), "Analise esse comportamento", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if estimate.Tokens.Input.Expected != 300 || estimate.Tokens.Output.Expected != 60 || estimate.ModelTime.Milliseconds.Expected != 5000 {
		t.Fatalf("expected independent medians from history; got tokens=%+v time=%+v", estimate.Tokens, estimate.ModelTime)
	}
	if estimate.Tokens.Confidence.Level != "MEDIUM" || estimate.Review.Confidence.Level != "LOW" {
		t.Fatalf("confidence must reflect separate evidence sources: tokens=%+v review=%+v", estimate.Tokens.Confidence, estimate.Review.Confidence)
	}
}

func TestTaskEstimatorUsesServerProfileAndCurrentWorkerSnapshot(t *testing.T) {
	agentID, _ := domain.NewAgentID("general")
	agent, _ := domain.NewAgentDefinition(agentID, domain.AgentTypeGeneral, "1", "helpful", "codex-default", nil, "General", "")
	projectID := uuid.New()
	repo := estimateSamples{capacity: &TaskEstimateCapacity{ProfileID: WorkspaceSmallProfileID, AvailableSlots: 0, MaximumSlots: 1}}
	estimator := NewTaskEstimator(estimateAgents{agent: agent}, nil, repo)
	estimate, err := estimator.Estimate(context.Background(), "Corrija o bug no backend", nil, &projectID)
	if err != nil {
		t.Fatal(err)
	}
	if !estimate.Hardware.Required || estimate.Hardware.ProfileID != WorkspaceSmallProfileID || estimate.Hardware.Concurrency.Expected != 1 {
		t.Fatalf("code task should use server-selected workspace profile: %+v", estimate.Hardware)
	}
	if estimate.Hardware.WorkerCapacity == nil || estimate.Hardware.WorkerCapacity.AvailableSlots != 0 {
		t.Fatalf("latest capacity snapshot should be included: %+v", estimate.Hardware.WorkerCapacity)
	}
	if estimate.Tokens.Confidence.Level != "LOW" || estimate.ModelTime.Confidence.Level != "LOW" {
		t.Fatalf("cold-start history must remain visibly low confidence: %+v %+v", estimate.Tokens.Confidence, estimate.ModelTime.Confidence)
	}
}

func TestTaskEstimatorKeepsTokenHistoryWhenDurationSamplesAreMissing(t *testing.T) {
	agentID, _ := domain.NewAgentID("general")
	agent, _ := domain.NewAgentDefinition(agentID, domain.AgentTypeGeneral, "1", "helpful", "codex-default", nil, "General", "")
	repo := estimateSamples{samples: []TaskEstimateSample{
		{InputTokens: 100, OutputTokens: 25}, {InputTokens: 200, OutputTokens: 50}, {InputTokens: 300, OutputTokens: 75},
		{InputTokens: 400, OutputTokens: 100}, {InputTokens: 500, OutputTokens: 125},
	}}
	estimate, err := NewTaskEstimator(estimateAgents{agent: agent}, nil, repo).Estimate(context.Background(), "Faça uma análise", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if estimate.Tokens.Confidence.SampleCount != 5 || estimate.Tokens.Confidence.Level != "MEDIUM" {
		t.Fatalf("token history should be retained: %+v", estimate.Tokens.Confidence)
	}
	if estimate.ModelTime.Confidence.SampleCount != 0 || estimate.ModelTime.Confidence.Level != "LOW" {
		t.Fatalf("duration needs its own sample confidence: %+v", estimate.ModelTime.Confidence)
	}
}

func TestTaskEstimateClassifiesRiskAndContextBuckets(t *testing.T) {
	if got := classifyTaskKind("implementar endpoint e corrigir bug"); got != "CODE" {
		t.Fatalf("kind=%s", got)
	}
	if got := estimateContextBucket(9000); got != "M" {
		t.Fatalf("context bucket=%s", got)
	}
	risk, minutes, factors := reviewEstimate("migration de banco em produção", "CODE")
	if risk != "ALTO" || minutes.Expected < 90 || len(factors) < 3 {
		t.Fatalf("risk estimate=%s %+v factors=%v", risk, minutes, factors)
	}
}
