package api

import (
	"context"
	"net/http"
	"sort"

	"github.com/google/uuid"

	"github.com/exotermo/prelo-core/internal/domain"
)

// MeHandler is contratos G3 (docs/integracoes/sessao-mobile.md): who the caller is, in which
// workspace, and which projects they may open — the first call of the web dashboard and of the
// Work Control app. Only a person's session answers; integration and technical tokens get 403.
type MeHandler struct {
	users      meUsers
	projects   meProjects
	members    meMembers
	workspaces meWorkspaces
	sessions   meSessions
}

type meSessions interface {
	FindByID(ctx context.Context, id uuid.UUID) (domain.MobileSession, error)
}

// SetMobileSessions (PR-2): /me reports the app session (device) behind the token.
func (h *MeHandler) SetMobileSessions(sessions meSessions) { h.sessions = sessions }

type meUsers interface {
	FindByID(ctx context.Context, id domain.DashboardUserID) (domain.DashboardUser, error)
}
type meProjects interface {
	List(ctx context.Context) ([]domain.Project, error)
}
type meMembers interface {
	ListProjectsForUser(ctx context.Context, userID domain.DashboardUserID) ([]domain.Project, error)
}
type meWorkspaces interface {
	Current(ctx context.Context) (domain.Workspace, error)
}

func NewMeHandler(users meUsers, projects meProjects, members meMembers, workspaces meWorkspaces) *MeHandler {
	return &MeHandler{users: users, projects: projects, members: members, workspaces: workspaces}
}

func RegisterMeRoutes(mux *http.ServeMux, h *MeHandler) {
	mux.HandleFunc("GET /api/v1/me", h.Get)
}

type meProject struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	ClientID *string `json:"clientId"`
}

type meSession struct {
	Kind       string  `json:"kind"`
	DeviceID   *string `json:"deviceId"`
	DeviceName *string `json:"deviceName"`
}

type meResponse struct {
	UserID        string      `json:"userId"`
	Email         string      `json:"email"`
	Role          string      `json:"role"`
	Scopes        []string    `json:"scopes"`
	WorkspaceID   string      `json:"workspaceId"`
	WorkspaceName string      `json:"workspaceName"`
	Projects      []meProject `json:"projects"`
	Session       meSession   `json:"session"`
}

func (h *MeHandler) Get(w http.ResponseWriter, r *http.Request) {
	identity, ok := FromContext(r.Context())
	if !ok {
		writeAuthError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if identity.TokenUse != "dashboard" {
		writeAuthError(w, http.StatusForbidden, "a personal session is required")
		return
	}
	userID, err := uuid.Parse(identity.Subject)
	if err != nil {
		writeAuthError(w, http.StatusForbidden, "a personal session is required")
		return
	}
	user, err := h.users.FindByID(r.Context(), domain.DashboardUserID{Value: userID})
	if err != nil {
		writeError(w, err)
		return
	}
	workspace, err := h.workspaces.Current(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	var projects []domain.Project
	if identity.HasScope("projects:manage") {
		projects, err = h.projects.List(r.Context())
	} else {
		projects, err = h.members.ListProjectsForUser(r.Context(), user.ID)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	scopes := make([]string, 0, len(identity.Scopes))
	for scope := range identity.Scopes {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	out := meResponse{
		UserID: user.ID.Value.String(), Email: user.Email, Role: string(user.Role), Scopes: scopes,
		WorkspaceID: workspace.ID.String(), WorkspaceName: workspace.Name,
		Projects: make([]meProject, 0, len(projects)),
		Session:  meSession{Kind: "web"},
	}
	if identity.SessionID != "" && h.sessions != nil {
		if sid, err := uuid.Parse(identity.SessionID); err == nil {
			if s, err := h.sessions.FindByID(r.Context(), sid); err == nil {
				device, name := s.DeviceID.String(), s.DeviceName
				out.Session = meSession{Kind: "mobile", DeviceID: &device, DeviceName: &name}
			}
		}
	}
	for _, p := range projects {
		out.Projects = append(out.Projects, meProject{ID: p.ID.String(), Name: p.Name, ClientID: clientIDString(p.ClientID)})
	}
	writeJSON(w, http.StatusOK, out)
}
