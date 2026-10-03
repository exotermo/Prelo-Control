package api

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/exotermo/hermes-app-go/internal/domain"
)

// gatewayAdmin is the narrow port over gateway.AdminClient.
type gatewayAdmin interface {
	Do(ctx context.Context, method, path string, body []byte) (int, []byte, error)
}

// ModelConnectionHandler (Fase M) relays the dashboard's model-connection screens to the
// llm-gateway's key vault. AGENTS.md: hermes-app never keeps provider keys — a key travels through
// this handler in a request body exactly once, untouched and unlogged, and every response is the
// gateway's key-free summary. Authorization is enforced here and in requiredScope: the instance
// connection needs settings:manage, a project's needs projects:manage to change and project
// membership (or projects:manage) to read.
type ModelConnectionHandler struct {
	gateway  gatewayAdmin
	projects projectFinder
	members  projectMembershipChecker
}

func NewModelConnectionHandler(gateway gatewayAdmin, projects projectFinder, members projectMembershipChecker) *ModelConnectionHandler {
	return &ModelConnectionHandler{gateway: gateway, projects: projects, members: members}
}

const maxConnectionBody = 16 * 1024

func (h *ModelConnectionHandler) relay(w http.ResponseWriter, r *http.Request, method, path string, withBody bool) {
	var body []byte
	if withBody {
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxConnectionBody))
		if err != nil {
			writeError(w, &domain.ValidationError{Message: "invalid request body"})
			return
		}
		body = raw
	}
	status, resp, err := h.gateway.Do(r.Context(), method, path, body)
	if err != nil {
		writeError(w, err)
		return
	}
	if status == http.StatusNoContent {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(resp)
}

// --- instance (settings:manage) ---

func (h *ModelConnectionHandler) GetInstance(w http.ResponseWriter, r *http.Request) {
	h.relay(w, r, http.MethodGet, "/instance", false)
}
func (h *ModelConnectionHandler) SaveInstance(w http.ResponseWriter, r *http.Request) {
	h.relay(w, r, http.MethodPut, "/instance", true)
}
func (h *ModelConnectionHandler) RetestInstance(w http.ResponseWriter, r *http.Request) {
	h.relay(w, r, http.MethodPost, "/instance/retest", false)
}
func (h *ModelConnectionHandler) DeleteInstance(w http.ResponseWriter, r *http.Request) {
	h.relay(w, r, http.MethodDelete, "/instance", false)
}
func (h *ModelConnectionHandler) Test(w http.ResponseWriter, r *http.Request) {
	h.relay(w, r, http.MethodPost, "/test", true)
}
func (h *ModelConnectionHandler) InstanceUsage(w http.ResponseWriter, r *http.Request) {
	h.relay(w, r, http.MethodGet, "/usage?days="+usageDays(r), false)
}

// --- project ---

func (h *ModelConnectionHandler) projectPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	project, _, ok := requireProjectMember(w, r, h.projects, h.members)
	return project.ID.String(), ok
}

func (h *ModelConnectionHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	if id, ok := h.projectPath(w, r); ok {
		h.relay(w, r, http.MethodGet, "/projects/"+id, false)
	}
}
func (h *ModelConnectionHandler) SaveProject(w http.ResponseWriter, r *http.Request) {
	if id, ok := h.projectPath(w, r); ok {
		h.relay(w, r, http.MethodPut, "/projects/"+id, true)
	}
}
func (h *ModelConnectionHandler) RetestProject(w http.ResponseWriter, r *http.Request) {
	if id, ok := h.projectPath(w, r); ok {
		h.relay(w, r, http.MethodPost, "/projects/"+id+"/retest", false)
	}
}
func (h *ModelConnectionHandler) SetProjectActive(w http.ResponseWriter, r *http.Request) {
	if id, ok := h.projectPath(w, r); ok {
		h.relay(w, r, http.MethodPut, "/projects/"+id+"/active", true)
	}
}
func (h *ModelConnectionHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	if id, ok := h.projectPath(w, r); ok {
		h.relay(w, r, http.MethodDelete, "/projects/"+id, false)
	}
}
func (h *ModelConnectionHandler) TestForProject(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.projectPath(w, r); ok {
		h.relay(w, r, http.MethodPost, "/test", true)
	}
}
func (h *ModelConnectionHandler) ProjectUsage(w http.ResponseWriter, r *http.Request) {
	if id, ok := h.projectPath(w, r); ok {
		h.relay(w, r, http.MethodGet, "/usage?projectId="+url.QueryEscape(id)+"&days="+usageDays(r), false)
	}
}

func usageDays(r *http.Request) string {
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days < 1 || days > 90 {
		days = 7
	}
	return strconv.Itoa(days)
}
