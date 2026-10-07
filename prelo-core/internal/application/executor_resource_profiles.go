package application

// ExecutorResourceProfile is a server-owned resource envelope. It is descriptive policy until a
// worker advertises and enforces the same profile; callers must not treat it as a capacity grant.
type ExecutorResourceProfile struct {
	ID                        string `json:"id"`
	MemoryBytes               int64  `json:"memoryBytes"`
	CPUQuotaMilli             int64  `json:"cpuQuotaMilli"`
	DiskBytes                 int64  `json:"diskBytes"`
	PIDs                      int    `json:"pids"`
	MaxRuntimeSeconds         int    `json:"maxRuntimeSeconds"`
	MaxContainersPerExecution int    `json:"maxContainersPerExecution"`
}

const WorkspaceSmallProfileID = "workspace-small-v1"

var executorResourceProfiles = map[string]ExecutorResourceProfile{
	WorkspaceSmallProfileID: {
		ID: WorkspaceSmallProfileID, MemoryBytes: 256 << 20, CPUQuotaMilli: 500,
		DiskBytes: 1 << 30, PIDs: 64, MaxRuntimeSeconds: 600, MaxContainersPerExecution: 1,
	},
}

// ExecutorResourceProfileForOperation deliberately resolves on the server. The model and API
// caller cannot choose a profile or increase its limits.
func ExecutorResourceProfileForOperation(operation string) (ExecutorResourceProfile, bool) {
	switch operation {
	case "START_WORKSPACE", "LIST", "READ", "MKDIR", "CREATE":
		profile, ok := executorResourceProfiles[WorkspaceSmallProfileID]
		return profile, ok
	default:
		return ExecutorResourceProfile{}, false
	}
}

func ExecutorResourceProfiles() []ExecutorResourceProfile {
	return []ExecutorResourceProfile{executorResourceProfiles[WorkspaceSmallProfileID]}
}
