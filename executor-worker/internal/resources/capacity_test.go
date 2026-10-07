package resources

import (
	"math"
	"testing"
)

func TestAssessUsesTightestResourceAndPreservesHostReserves(t *testing.T) {
	s := Snapshot{CgroupsVersion: "v2", MemoryAvailBytes: 3 << 30, CgroupMemoryMax: 2 << 30, CgroupMemoryBytes: 512 << 20,
		CPUCount: 2, CgroupCPUCount: 2, PIDsMax: 512, PIDsCurrent: 64, DiskAvailBytes: 20 << 30}
	policy := Policy{MemoryReserveBytes: 512 << 20, CPUReserve: 1, DiskReserveBytes: 2 << 30, PIDsReserve: 64, MaxContainers: 8}
	profile := Profile{MemoryBytes: 256 << 20, CPU: .5, DiskBytes: 1 << 30, PIDs: 64}
	got, err := Assess(s, policy, profile)
	if err != nil {
		t.Fatal(err)
	}
	// Effective cgroup memory: 1.5 GiB; after 512 MiB reserve, four slots remain.
	// CPU reserve permits two slots and is therefore the limiting resource.
	if !got.CanStart || got.Slots != 2 || got.Reason != "AVAILABLE" {
		t.Fatalf("unexpected assessment: %+v", got)
	}
}

func TestAssessWithActiveContainersReturnsOnlyAdditionalSlots(t *testing.T) {
	s := Snapshot{CgroupsVersion: "v2", MemoryAvailBytes: 4 << 30, CgroupMemoryMax: math.MaxInt64,
		CPUCount: 4, CgroupCPUCount: 4, PIDsMax: 1024, PIDsCurrent: 0, DiskAvailBytes: 20 << 30}
	policy := Policy{MemoryReserveBytes: 0, CPUReserve: 0, DiskReserveBytes: 0, PIDsReserve: 0, MaxContainers: 4}
	profile := Profile{MemoryBytes: 256 << 20, CPU: .5, DiskBytes: 1 << 30, PIDs: 64}

	for _, tc := range []struct {
		active int
		want   int
	}{
		{active: 0, want: 4}, // admin ceiling is tighter than each resource budget
		{active: 1, want: 3},
		{active: 4, want: 0},
		{active: 6, want: 0}, // a lowered admin cap never admits additional containers
	} {
		got, err := AssessWithActive(s, policy, profile, tc.active)
		if err != nil {
			t.Fatal(err)
		}
		if got.Slots != tc.want || got.CanStart != (tc.want > 0) {
			t.Fatalf("active=%d: got %+v, want %d additional slots", tc.active, got, tc.want)
		}
		if tc.want == 0 && got.Reason != "CONTAINER_LIMIT" {
			t.Fatalf("active=%d: expected container limit reason, got %+v", tc.active, got)
		}
	}
}

func TestAssessWithActiveContainersStillUsesTightestHardwareResource(t *testing.T) {
	s := Snapshot{CgroupsVersion: "v2", MemoryAvailBytes: 2 << 30, CgroupMemoryMax: math.MaxInt64,
		CPUCount: 2, CgroupCPUCount: 2, PIDsMax: 1024, PIDsCurrent: 0, DiskAvailBytes: 20 << 30}
	policy := Policy{CPUReserve: 1, MaxContainers: 8}
	profile := Profile{MemoryBytes: 256 << 20, CPU: .5, DiskBytes: 1 << 30, PIDs: 64}
	got, err := AssessWithActive(s, policy, profile, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Available CPU admits two total containers; one is already active.
	if got.Slots != 1 || !got.CanStart {
		t.Fatalf("active container was not subtracted from CPU capacity: %+v", got)
	}
}

func TestAssessWithNegativeActiveContainerCountFailsClosed(t *testing.T) {
	s := Snapshot{CgroupsVersion: "v2", MemoryAvailBytes: 1 << 30, CgroupMemoryMax: math.MaxInt64,
		CPUCount: 2, CgroupCPUCount: 2, PIDsMax: math.MaxInt64, DiskAvailBytes: 4 << 30}
	_, err := AssessWithActive(s, Policy{MaxContainers: 2}, Profile{MemoryBytes: 1, CPU: .5, DiskBytes: 1, PIDs: 1}, -1)
	if err == nil {
		t.Fatal("negative active count was accepted")
	}
}

func TestMaximumSlotsUsesStructuralResourcesAndAdminCeiling(t *testing.T) {
	s := Snapshot{CgroupsVersion: "v2", MemoryTotalBytes: 4 << 30, CgroupMemoryMax: math.MaxInt64,
		CPUCount: 2, CgroupCPUCount: 2, PIDsMax: 1024, DiskTotalBytes: 32 << 30}
	policy := Policy{MemoryReserveBytes: 1 << 30, CPUReserve: 1, DiskReserveBytes: 4 << 30, PIDsReserve: 64, MaxContainers: 8}
	profile := Profile{MemoryBytes: 256 << 20, CPU: .5, DiskBytes: 1 << 30, PIDs: 64}
	got, err := MaximumSlots(s, policy, profile)
	if err != nil {
		t.Fatal(err)
	}
	// Memory allows 12; CPU allows 2; disk and PIDs allow more. CPU is the bottleneck.
	if got != 2 {
		t.Fatalf("maximum slots=%d, want 2", got)
	}
	policy.CPUReserve = 2
	got, err = MaximumSlots(s, policy, profile)
	if err != nil || got != 0 {
		t.Fatalf("structurally unsupported profile got %d slots, err=%v", got, err)
	}
}

func TestMaximumSlotsDoesNotTreatUnlimitedPIDsAsZeroCapacity(t *testing.T) {
	s := Snapshot{CgroupsVersion: "v2", MemoryTotalBytes: 4 << 30, CgroupMemoryMax: math.MaxInt64,
		CPUCount: 2, CgroupCPUCount: 2, PIDsMax: math.MaxInt64, DiskTotalBytes: 32 << 30}
	policy := Policy{MemoryReserveBytes: 1 << 30, CPUReserve: .5, DiskReserveBytes: 4 << 30, PIDsReserve: 128, MaxContainers: 4}
	profile := Profile{MemoryBytes: 256 << 20, CPU: .5, DiskBytes: 1 << 30, PIDs: 64}
	got, err := MaximumSlots(s, policy, profile)
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Fatalf("unlimited PID controller should defer to other resources and admin cap; got %d, want 3", got)
	}
}

func TestAssessBlocksWhenMemoryOrDiskReserveWouldBeConsumed(t *testing.T) {
	base := Snapshot{CgroupsVersion: "v2", MemoryAvailBytes: 768 << 20, CgroupMemoryMax: math.MaxInt64, CgroupMemoryBytes: 0,
		CPUCount: 2, CgroupCPUCount: 2, PIDsMax: math.MaxInt64, DiskAvailBytes: 8 << 30}
	policy := Policy{MemoryReserveBytes: 768 << 20, CPUReserve: .5, DiskReserveBytes: 2 << 30, MaxContainers: 2}
	profile := Profile{MemoryBytes: 256 << 20, CPU: .5, DiskBytes: 1 << 30, PIDs: 64}
	got, err := Assess(base, policy, profile)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanStart || got.Slots != 0 || got.Reason != "MEMORY_RESERVE" {
		t.Fatalf("memory pressure should block admission: %+v", got)
	}

	base.MemoryAvailBytes = 2 << 30
	base.DiskAvailBytes = 2 << 30
	got, err = Assess(base, policy, profile)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanStart || got.Reason != "DISK_RESERVE" {
		t.Fatalf("disk reserve should block admission: %+v", got)
	}
}

func TestAssessFailsClosedOnMissingOrInvalidMeasurements(t *testing.T) {
	_, err := Assess(Snapshot{}, Policy{MaxContainers: 1}, Profile{MemoryBytes: 1, CPU: 1, DiskBytes: 1, PIDs: 1})
	if err == nil {
		t.Fatal("accepted unknown capacity")
	}
	_, err = Assess(Snapshot{CgroupsVersion: "v1"}, Policy{MaxContainers: 1}, Profile{MemoryBytes: 1, CPU: 1, DiskBytes: 1, PIDs: 1})
	if err == nil {
		t.Fatal("accepted cgroups v1")
	}
}
