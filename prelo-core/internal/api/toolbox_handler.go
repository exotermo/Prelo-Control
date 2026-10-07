package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
)

type toolboxAgents interface {
	List() []domain.AgentDefinition
}

type ToolboxHandler struct {
	projects        projectFinder
	members         projectMembershipChecker
	tools           application.ToolRegistry
	agents          toolboxAgents
	settings        application.ToolSettings
	executorEnabled bool
	workers         *application.ExecutorWorkerService
}

func NewToolboxHandler(projects projectFinder, members projectMembershipChecker, tools application.ToolRegistry, agents toolboxAgents, settings application.ToolSettings) *ToolboxHandler {
	return &ToolboxHandler{projects: projects, members: members, tools: tools, agents: agents, settings: settings}
}
func (h *ToolboxHandler) SetExecutorEnabled(enabled bool) { h.executorEnabled = enabled }
func (h *ToolboxHandler) SetExecutorWorkers(workers *application.ExecutorWorkerService) {
	h.workers = workers
}

type toolboxTool struct {
	Name            string                               `json:"name"`
	Description     string                               `json:"description"`
	RiskLevel       string                               `json:"riskLevel"`
	Impact          string                               `json:"impact"`
	Enabled         bool                                 `json:"enabled"`
	Version         int64                                `json:"version"`
	Agents          []string                             `json:"agents"`
	ResourceProfile *application.ExecutorResourceProfile `json:"resourceProfile,omitempty"`
}

func (h *ToolboxHandler) List(w http.ResponseWriter, r *http.Request) {
	project, identity, ok := requireProjectMember(w, r, h.projects, h.members)
	if !ok {
		return
	}
	if identity.TokenUse != "dashboard" {
		writeAuthError(w, http.StatusForbidden, "only a human session can inspect tool settings")
		return
	}
	result := struct {
		ProjectID          string                                `json:"projectId"`
		Tools              []toolboxTool                         `json:"tools"`
		WorkerStatus       string                                `json:"workerStatus"`
		ResourceProfiles   []application.ExecutorResourceProfile `json:"resourceProfiles"`
		ProfileEnforcement string                                `json:"profileEnforcement"`
	}{ProjectID: project.ID.String(), Tools: []toolboxTool{}, WorkerStatus: "DISABLED", ResourceProfiles: application.ExecutorResourceProfiles(), ProfileEnforcement: "DEFINED_NOT_ENFORCED"}
	if h.executorEnabled {
		result.WorkerStatus = "NO_WORKER"
		if h.workers != nil {
			workers, err := h.workers.List(r.Context())
			if err != nil {
				writeError(w, err)
				return
			}
			for _, worker := range workers {
				if worker.ProjectID == project.ID && worker.Enabled && worker.LastSeenAt != nil && time.Since(*worker.LastSeenAt) <= 30*time.Second {
					result.WorkerStatus = "READY"
					break
				}
			}
		}
	}
	defs := h.tools.List()
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	for _, def := range defs {
		setting, err := h.settings.Get(r.Context(), project.ID, def.Name)
		if err != nil {
			writeError(w, err)
			return
		}
		item := toolboxTool{Name: def.Name, Description: def.Description, RiskLevel: string(def.RiskLevel), Impact: def.Impact, Enabled: setting.Enabled, Version: setting.Version, Agents: []string{}}
		for _, agent := range h.agents.List() {
			for _, capability := range agent.Capabilities {
				if capability == def.Name {
					item.Agents = append(item.Agents, agent.AgentID.String())
					break
				}
			}
		}
		result.Tools = append(result.Tools, item)
	}
	remote := []struct{ name, description, operation string }{
		{"workspace_start", "Iniciar um contêiner isolado para esta execução", "START_WORKSPACE"},
		{"workspace_list", "Listar arquivos dentro do contêiner da task", "LIST"},
		{"workspace_read", "Ler um arquivo dentro do contêiner da task", "READ"},
		{"workspace_mkdir", "Criar uma pasta no contêiner da task", "MKDIR"},
		{"workspace_create_file", "Criar e publicar arquivo no projeto", "CREATE"},
	}
	for _, entry := range remote {
		setting, err := h.settings.Get(r.Context(), project.ID, entry.name)
		if err != nil {
			writeError(w, err)
			return
		}
		profile, _ := application.ExecutorResourceProfileForOperation(entry.operation)
		result.Tools = append(result.Tools, toolboxTool{Name: entry.name, Description: entry.description, RiskLevel: "HIGH", Impact: "Exige aprovação administrativa para cada operação; worker em VM dedicada", Enabled: setting.Enabled && setting.Version > 0, Version: setting.Version, Agents: []string{}, ResourceProfile: &profile})
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *ToolboxHandler) Set(w http.ResponseWriter, r *http.Request) {
	project, identity, ok := requireProjectMember(w, r, h.projects, h.members)
	if !ok {
		return
	}
	if identity.TokenUse != "dashboard" || !identity.HasScope("projects:manage") {
		writeAuthError(w, http.StatusForbidden, "only a human administrator can configure tools")
		return
	}
	name := r.PathValue("toolName")
	_, found := h.tools.Find(name)
	if !found && !application.IsExecutorTool(name) {
		writeError(w, application.ErrToolNotFound)
		return
	}
	var req struct {
		Enabled         *bool  `json:"enabled"`
		ExpectedVersion *int64 `json:"expectedVersion"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Enabled == nil || req.ExpectedVersion == nil || *req.ExpectedVersion < 0 {
		writeError(w, &domain.ValidationError{Message: "enabled and non-negative expectedVersion are required"})
		return
	}
	setting, err := h.settings.Set(r.Context(), project.ID, name, *req.Enabled, *req.ExpectedVersion, identity.Subject)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Enabled bool  `json:"enabled"`
		Version int64 `json:"version"`
	}{setting.Enabled, setting.Version})
}
