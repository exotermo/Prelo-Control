package application

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/google/uuid"
)

type EstimateRange struct {
	Min      int `json:"min"`
	Expected int `json:"expected"`
	Max      int `json:"max"`
}

type EstimateConfidence struct {
	Level       string `json:"level"`
	SampleCount int    `json:"sampleCount"`
	Basis       string `json:"basis"`
}

type TaskEstimateSample struct {
	InputTokens  int
	OutputTokens int
	DurationMs   int64
}

type TaskEstimateCapacity struct {
	ProfileID      string    `json:"profileId"`
	AvailableSlots int       `json:"availableSlots"`
	MaximumSlots   int       `json:"maximumSlots"`
	Active         int       `json:"activeContainers"`
	ObservedAt     time.Time `json:"observedAt"`
}

type TaskEstimateRepository interface {
	Samples(context.Context, string, string, string, int) ([]TaskEstimateSample, error)
	Capacity(context.Context, uuid.UUID) (*TaskEstimateCapacity, error)
}

type TaskEstimate struct {
	TaskKind     string                `json:"taskKind"`
	ModelProfile string                `json:"modelProfile"`
	ContextSize  string                `json:"contextSize"`
	Hardware     HardwareTaskEstimate  `json:"hardware"`
	Tokens       TokensTaskEstimate    `json:"tokens"`
	ModelTime    ModelTimeTaskEstimate `json:"modelTime"`
	Review       ReviewTaskEstimate    `json:"reviewAndTests"`
	ObservedAt   time.Time             `json:"observedAt"`
}

type HardwareTaskEstimate struct {
	Required       bool                  `json:"required"`
	ProfileID      string                `json:"profileId,omitempty"`
	CPUMilli       int64                 `json:"cpuMilli,omitempty"`
	MemoryBytes    int64                 `json:"memoryBytes,omitempty"`
	TemporaryBytes int64                 `json:"temporaryBytes,omitempty"`
	Concurrency    EstimateRange         `json:"concurrency"`
	WorkerCapacity *TaskEstimateCapacity `json:"workerCapacity,omitempty"`
	Confidence     EstimateConfidence    `json:"confidence"`
}

type TokensTaskEstimate struct {
	Input      EstimateRange      `json:"input"`
	Output     EstimateRange      `json:"output"`
	Confidence EstimateConfidence `json:"confidence"`
}

type ModelTimeTaskEstimate struct {
	Milliseconds EstimateRange      `json:"milliseconds"`
	Confidence   EstimateConfidence `json:"confidence"`
}

type ReviewTaskEstimate struct {
	Minutes    EstimateRange      `json:"minutes"`
	Risk       string             `json:"risk"`
	Factors    []string           `json:"factors"`
	Confidence EstimateConfidence `json:"confidence"`
}

type TaskEstimator struct {
	agents   AgentRegistry
	projects ProjectReader
	repo     TaskEstimateRepository
}

func NewTaskEstimator(agents AgentRegistry, projects ProjectReader, repo TaskEstimateRepository) *TaskEstimator {
	return &TaskEstimator{agents: agents, projects: projects, repo: repo}
}

func (s *TaskEstimator) Estimate(ctx context.Context, description string, agentID *domain.AgentID, projectID *uuid.UUID) (TaskEstimate, error) {
	description = strings.TrimSpace(description)
	if len(description) == 0 || len(description) > domain.MaxDescriptionLength {
		return TaskEstimate{}, &domain.ValidationError{Message: "description must contain 1 to 4000 characters"}
	}
	resolved := domain.AgentID{Value: "general"}
	if agentID != nil && agentID.Value != "" {
		resolved = *agentID
	} else if projectID != nil && s.projects != nil {
		project, err := s.projects.FindByID(ctx, domain.ProjectID{Value: *projectID})
		if err != nil {
			return TaskEstimate{}, err
		}
		if project.DefaultAgentID != nil {
			resolved = domain.AgentID{Value: *project.DefaultAgentID}
		}
	}
	agent, err := s.agents.FindRequired(resolved)
	if err != nil {
		return TaskEstimate{}, err
	}
	taskKind := classifyTaskKind(description)
	// Predict the fixed system/tool envelope as well as the task text so the estimate queries the
	// same context-size bucket used by completed Gateway calls. It is intentionally approximate.
	contextChars := 5000 + len(description) + len(agent.Directive)
	if taskKind == "CODE" || taskKind == "FILES" {
		contextChars += 6000
	}
	contextTokens := max(1, (contextChars+3)/4)
	contextSize := estimateContextBucket(contextTokens)
	samples, err := s.repo.Samples(ctx, agent.ModelProfile, taskKind, contextSize, 250)
	if err != nil {
		return TaskEstimate{}, err
	}
	input, output, duration, durationSamples := modelEstimateRanges(samples, contextTokens, taskKind)
	modelConfidence := historicalConfidence(len(samples), "faixa inicial baseada no tamanho do contexto; sem amostra suficiente")
	if len(samples) > 0 {
		modelConfidence = historicalConfidence(len(samples), "histórico agregado do mesmo perfil, tipo de task e faixa de contexto")
	}
	durationConfidence := historicalConfidence(durationSamples, "faixa inicial de duração; sem amostra suficiente")
	if durationSamples > 0 {
		durationConfidence = historicalConfidence(durationSamples, "histórico de duração do mesmo perfil, tipo de task e faixa de contexto")
	}

	estimate := TaskEstimate{TaskKind: taskKind, ModelProfile: agent.ModelProfile, ContextSize: contextSize, ObservedAt: time.Now().UTC(),
		Tokens:    TokensTaskEstimate{Input: input, Output: output, Confidence: modelConfidence},
		ModelTime: ModelTimeTaskEstimate{Milliseconds: duration, Confidence: durationConfidence}}
	needsWorkspace := taskKind == "CODE" || taskKind == "FILES"
	estimate.Hardware = HardwareTaskEstimate{Required: needsWorkspace,
		Concurrency: EstimateRange{Min: boolInt(needsWorkspace), Expected: boolInt(needsWorkspace), Max: boolInt(needsWorkspace)},
		Confidence:  EstimateConfidence{Level: "LOW", SampleCount: 0, Basis: "perfil declarado pelo servidor; consumo real por task ainda não está disponível"}}
	if needsWorkspace {
		if profile, ok := ExecutorResourceProfileForOperation("START_WORKSPACE"); ok {
			estimate.Hardware.ProfileID = profile.ID
			estimate.Hardware.CPUMilli = profile.CPUQuotaMilli
			estimate.Hardware.MemoryBytes = profile.MemoryBytes
			estimate.Hardware.TemporaryBytes = profile.DiskBytes
		}
	}
	if projectID != nil {
		capacity, err := s.repo.Capacity(ctx, *projectID)
		if err != nil {
			return TaskEstimate{}, err
		}
		if capacity != nil {
			estimate.Hardware.WorkerCapacity = capacity
		}
	}

	risk, reviewRange, factors := reviewEstimate(description, taskKind)
	estimate.Review = ReviewTaskEstimate{Minutes: reviewRange, Risk: risk, Factors: factors,
		Confidence: EstimateConfidence{Level: "LOW", SampleCount: 0, Basis: "heurística inicial; ainda não há histórico de revisão/testes humanos"}}
	return estimate, nil
}

func classifyTaskKind(description string) string {
	d := strings.ToLower(description)
	if containsAny(d, "código", "codigo", "code", "bug", "refator", "implement", "program", "api", "backend", "frontend", "deploy", "teste unit", "repository", "repositório") {
		return "CODE"
	}
	if containsAny(d, "arquivo", "documento", "pasta", "workspace", "planilha", "relatório", "relatorio") {
		return "FILES"
	}
	if containsAny(d, "pesquis", "compare", "analis", "investig", "diagnóst", "diagnost") {
		return "ANALYSIS"
	}
	return "GENERAL"
}

func estimateContextBucket(tokens int) string {
	switch {
	case tokens < 2048:
		return "XS"
	case tokens < 8192:
		return "S"
	case tokens < 24576:
		return "M"
	default:
		return "L"
	}
}

func modelEstimateRanges(samples []TaskEstimateSample, inputTokens int, kind string) (EstimateRange, EstimateRange, EstimateRange, int) {
	if len(samples) == 0 {
		output := 700
		if kind == "CODE" || kind == "FILES" {
			output = 1800
		} else if kind == "ANALYSIS" {
			output = 1200
		}
		return EstimateRange{Min: inputTokens, Expected: inputTokens * 2, Max: inputTokens * 5},
			EstimateRange{Min: output / 2, Expected: output, Max: output * 3},
			EstimateRange{Min: 20_000, Expected: 90_000, Max: 300_000}, 0
	}
	inputs, outputs, durations := make([]int, 0, len(samples)), make([]int, 0, len(samples)), make([]int, 0, len(samples))
	for _, sample := range samples {
		inputs = append(inputs, sample.InputTokens)
		outputs = append(outputs, sample.OutputTokens)
		if sample.DurationMs > 0 {
			durations = append(durations, int(sample.DurationMs))
		}
	}
	durationRange := EstimateRange{Min: 20_000, Expected: 90_000, Max: 300_000}
	if len(durations) > 0 {
		durationRange = quantileRange(durations)
	}
	return quantileRange(inputs), quantileRange(outputs), durationRange, len(durations)
}

func quantileRange(values []int) EstimateRange {
	sorted := append([]int(nil), values...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return EstimateRange{Min: percentile(sorted, .2), Expected: percentile(sorted, .5), Max: percentile(sorted, .8)}
}

func percentile(values []int, p float64) int {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Round(p * float64(len(values)-1)))
	return values[index]
}

func historicalConfidence(count int, basis string) EstimateConfidence {
	level := "LOW"
	if count >= 20 {
		level = "HIGH"
	} else if count >= 5 {
		level = "MEDIUM"
	}
	return EstimateConfidence{Level: level, SampleCount: count, Basis: basis}
}

func reviewEstimate(description, kind string) (string, EstimateRange, []string) {
	risk := "BAIXO"
	minMinutes, expected, maxMinutes := 5, 15, 30
	factors := []string{"tipo de task", "tamanho da descrição"}
	if kind == "CODE" || kind == "FILES" {
		minMinutes, expected, maxMinutes = 15, 45, 120
		factors = append(factors, "possíveis arquivos afetados", "validação de build/testes")
	}
	if containsAny(strings.ToLower(description), "produção", "permissão", "permissao", "segredo", "autenticação", "autenticacao", "pagamento", "delete", "excluir", "migrar banco", "migration") {
		risk = "ALTO"
		minMinutes *= 2
		expected *= 2
		maxMinutes *= 2
		factors = append(factors, "palavras associadas a risco elevado")
	}
	if len(description) > 1500 {
		expected += 15
		maxMinutes += 30
		factors = append(factors, "descrição extensa")
	}
	return risk, EstimateRange{Min: minMinutes, Expected: expected, Max: maxMinutes}, factors
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
