package domain

import "time"

const MaxProjectNameLength = 120

// Project is a Fase W work environment — a named grouping that scopes Tasks and Servers (and,
// by extension, anything derived from Task: Pipeline, Approvals) to one context, so the same
// Prelo instance can keep a personal client's SaaS separate from a work project. Membership
// (ProjectMember) is the actual access boundary; Project itself just carries the metadata.
type Project struct {
	ID          ProjectID
	Name        string
	Description *string
	CreatedAt   time.Time
	CreatedBy   string
	Version     int64
	DeletedAt   *time.Time
	// Fase PA settings. DefaultAgentID is used by tasks of this project that don't pick an agent;
	// Instructions reach every agent of the project as a context item; CoverColor tints the cover.
	DefaultAgentID *string
	Instructions   *string
	CoverColor     string
	// Fase C1: the client this project is for (optional; set via SetProjectClient).
	ClientID *ClientID
}

const MaxProjectInstructionsLength = 4000

// ProjectCoverColors is the fixed palette the dashboard knows how to render.
var ProjectCoverColors = map[string]bool{"ink": true, "clay": true, "moss": true, "ocean": true, "plum": true, "mustard": true}

// WithSettings applies the editable settings after validating them (the default agent's
// existence in the catalog is checked by the caller, which owns the registry).
func (p Project) WithSettings(name string, description, defaultAgentID, instructions *string, coverColor string) (Project, error) {
	if isBlank(name) || len(name) > MaxProjectNameLength {
		return Project{}, &ValidationError{Message: "project name is required and must be at most 120 characters"}
	}
	if instructions != nil && len(*instructions) > MaxProjectInstructionsLength {
		return Project{}, &ValidationError{Message: "instructions must be at most 4000 characters"}
	}
	if !ProjectCoverColors[coverColor] {
		return Project{}, &ValidationError{Message: "unknown cover color"}
	}
	p.Name = name
	p.Description = blankToNil(description)
	p.DefaultAgentID = blankToNil(defaultAgentID)
	p.Instructions = blankToNil(instructions)
	p.CoverColor = coverColor
	return p, nil
}

func blankToNil(s *string) *string {
	if s == nil || isBlank(*s) {
		return nil
	}
	return s
}

func NewProject(name string, description *string, createdBy string) (Project, error) {
	if isBlank(name) || len(name) > MaxProjectNameLength {
		return Project{}, &ValidationError{Message: "project name is required and must be at most 120 characters"}
	}
	return Project{
		ID:          NewProjectID(),
		Name:        name,
		Description: description,
		CreatedAt:   time.Now().UTC(),
		CreatedBy:   createdBy,
		CoverColor:  "ink",
	}, nil
}
