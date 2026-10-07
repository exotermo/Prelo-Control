package domain

import "time"

// ProjectMember is binary membership — no role within the project. Only a global ADMIN
// (projects:manage) ever adds/removes a row here; an OPERATOR's visibility into a Project's
// Tasks/Servers is gated entirely by whether this row exists (see ProjectMemberRepository.IsMember).
type ProjectMember struct {
	ProjectID ProjectID
	UserID    DashboardUserID
	Role      string
	AddedAt   time.Time
	AddedBy   string
}

func NewProjectMember(projectID ProjectID, userID DashboardUserID, addedBy string) ProjectMember {
	return ProjectMember{ProjectID: projectID, UserID: userID, Role: "MEMBER", AddedAt: time.Now().UTC(), AddedBy: addedBy}
}
