package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/application"
	"github.com/exotermo/prelo-core/internal/domain"
	"github.com/exotermo/prelo-core/internal/infrastructure/toolregistry"
	"github.com/exotermo/prelo-core/internal/infrastructure/tools"
)

type toolboxSettingsFake struct {
	settings map[string]application.ToolSetting
}

func (f *toolboxSettingsFake) Get(_ context.Context, _ domain.ProjectID, name string) (application.ToolSetting, error) {
	if setting, ok := f.settings[name]; ok {
		return setting, nil
	}
	return application.ToolSetting{Enabled: true}, nil
}
func (f *toolboxSettingsFake) Allowed(ctx context.Context, id domain.ProjectID, name string) (bool, error) {
	setting, err := f.Get(ctx, id, name)
	return setting.Enabled, err
}
func (f *toolboxSettingsFake) Set(_ context.Context, _ domain.ProjectID, name string, enabled bool, expected int64, _ string) (application.ToolSetting, error) {
	current, _ := f.Get(context.Background(), domain.ProjectID{}, name)
	if current.Version != expected {
		return application.ToolSetting{}, application.ErrOptimisticLock
	}
	next := application.ToolSetting{Enabled: enabled, Version: expected + 1}
	f.settings[name] = next
	return next, nil
}

type toolboxAgentList []domain.AgentDefinition

func (a toolboxAgentList) List() []domain.AgentDefinition { return a }

func TestToolbox_ProjectMembershipAndHumanAdmin(t *testing.T) {
	project, err := domain.NewProject("Projeto", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	userID := uuid.New()
	agentID, _ := domain.NewAgentID("general")
	agent, err := domain.NewAgentDefinition(agentID, domain.AgentTypeGeneral, "1", "directive", "profile", []string{"current_time"}, "General", "desc")
	if err != nil {
		t.Fatal(err)
	}
	settings := &toolboxSettingsFake{settings: map[string]application.ToolSetting{}}
	h := NewToolboxHandler(fakeProjectFinder{projects: map[string]domain.Project{project.ID.String(): project}},
		fakeMembershipChecker{members: map[string]bool{project.ID.String() + "/" + userID.String(): true}},
		toolregistry.NewStatic(tools.NewCurrentTimeTool()), toolboxAgentList{agent}, settings)
	mux := http.NewServeMux()
	RegisterToolboxRoutes(mux, h)
	path := "/api/v1/projects/" + project.ID.String() + "/toolbox"
	call := func(method, suffix, body string, identity AuthContext) *httptest.ResponseRecorder {
		req := withIdentity(httptest.NewRequest(method, path+suffix, strings.NewReader(body)), identity)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	member := AuthContext{Subject: userID.String(), TokenUse: "dashboard", Scopes: map[string]struct{}{"projects:read": {}}}
	if w := call(http.MethodGet, "", "", member); w.Code != http.StatusOK {
		t.Fatalf("member cannot read toolbox: %d %s", w.Code, w.Body.String())
	} else {
		var response struct {
			Tools              []toolboxTool                         `json:"tools"`
			WorkerStatus       string                                `json:"workerStatus"`
			ResourceProfiles   []application.ExecutorResourceProfile `json:"resourceProfiles"`
			ProfileEnforcement string                                `json:"profileEnforcement"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Tools) != 6 || !response.Tools[0].Enabled || response.Tools[0].Version != 0 || response.WorkerStatus != "DISABLED" || len(response.ResourceProfiles) != 1 || response.ProfileEnforcement != "DEFINED_NOT_ENFORCED" {
			t.Fatalf("unexpected toolbox: %+v", response)
		}
		for _, tool := range response.Tools[1:] {
			if tool.Enabled || tool.Version != 0 {
				t.Fatalf("workspace tool enabled by default: %+v", tool)
			}
			if strings.HasPrefix(tool.Name, "workspace_") && (tool.ResourceProfile == nil || tool.ResourceProfile.ID != application.WorkspaceSmallProfileID) {
				t.Fatalf("workspace tool is missing its server profile: %+v", tool)
			}
		}
	}
	if w := call(http.MethodGet, "", "", AuthContext{Subject: uuid.NewString(), TokenUse: "dashboard", Scopes: member.Scopes}); w.Code != http.StatusForbidden {
		t.Fatalf("non-member got %d", w.Code)
	}
	if w := call(http.MethodGet, "", "", AuthContext{Subject: userID.String(), TokenUse: "api_key", Scopes: member.Scopes}); w.Code != http.StatusForbidden {
		t.Fatalf("machine read toolbox: %d", w.Code)
	}
	body := `{"enabled":false,"expectedVersion":0}`
	if w := call(http.MethodPut, "/current_time", body, member); w.Code != http.StatusForbidden {
		t.Fatalf("operator changed toolbox: %d", w.Code)
	}
	admin := AuthContext{Subject: userID.String(), TokenUse: "dashboard", Scopes: map[string]struct{}{"projects:manage": {}}}
	if w := call(http.MethodPut, "/current_time", body, AuthContext{Subject: userID.String(), TokenUse: "api_key", Scopes: admin.Scopes}); w.Code != http.StatusForbidden {
		t.Fatalf("machine changed toolbox: %d", w.Code)
	}
	if w := call(http.MethodPut, "/current_time", body, admin); w.Code != http.StatusOK {
		t.Fatalf("admin could not disable tool: %d %s", w.Code, w.Body.String())
	}
	if w := call(http.MethodPut, "/current_time", body, admin); w.Code != http.StatusConflict {
		t.Fatalf("stale update got %d", w.Code)
	}
	if settings.settings["current_time"].Enabled {
		t.Fatal("tool still enabled")
	}
	if requiredScope(http.MethodGet, path) != "projects:read" || requiredScope(http.MethodPut, path+"/current_time") != "projects:manage" {
		t.Fatal("toolbox scopes are not project-scoped")
	}
}
