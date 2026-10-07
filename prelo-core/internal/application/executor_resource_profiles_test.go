package application

import "testing"

func TestExecutorResourceProfileIsServerSelectedAndBounded(t *testing.T) {
	for _, operation := range []string{"START_WORKSPACE", "LIST", "READ", "MKDIR", "CREATE"} {
		profile, ok := ExecutorResourceProfileForOperation(operation)
		if !ok || profile.ID != WorkspaceSmallProfileID {
			t.Fatalf("operation %s did not resolve to the server profile: %+v, %v", operation, profile, ok)
		}
		if profile.MemoryBytes != 256<<20 || profile.CPUQuotaMilli != 500 || profile.DiskBytes != 1<<30 || profile.PIDs != 64 || profile.MaxContainersPerExecution != 1 {
			t.Fatalf("unexpected resource envelope: %+v", profile)
		}
	}
	if _, ok := ExecutorResourceProfileForOperation("SHELL"); ok {
		t.Fatal("unsupported operation received a resource profile")
	}
}

func TestExecutorResourceProfilesReturnsCopy(t *testing.T) {
	profiles := ExecutorResourceProfiles()
	profiles[0].MemoryBytes = 1
	resolved, _ := ExecutorResourceProfileForOperation("READ")
	if resolved.MemoryBytes != 256<<20 {
		t.Fatal("caller mutated the server resource profile catalog")
	}
}
