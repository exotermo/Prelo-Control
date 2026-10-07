package resources

import (
	"errors"
	"math"
)

var ErrInvalidPolicy = errors.New("invalid executor resource policy")

type Profile struct {
	MemoryBytes int64
	CPU         float64
	DiskBytes   int64
	PIDs        int64
}

type Policy struct {
	MemoryReserveBytes int64
	CPUReserve         float64
	DiskReserveBytes   int64
	PIDsReserve        int64
	MaxContainers      int
}

type Capacity struct {
	CanStart  bool
	Slots     int
	Reason    string
	MemoryFor int64
	CPUFor    float64
	DiskFor   int64
	PIDsFor   int64
}

// MaximumSlots estimates the worker's structural ceiling using total host/cgroup resources,
// reservations and the administrator's configured maximum. It excludes live usage/load so a
// temporary spike cannot incorrectly make a profile look unsupported forever.
func MaximumSlots(s Snapshot, policy Policy, profile Profile) (int, error) {
	if s.CgroupsVersion != "v2" || s.MemoryTotalBytes <= 0 || s.CPUCount <= 0 || s.CgroupCPUCount <= 0 || s.DiskTotalBytes <= 0 ||
		policy.MemoryReserveBytes < 0 || policy.CPUReserve < 0 || math.IsNaN(policy.CPUReserve) || math.IsInf(policy.CPUReserve, 0) ||
		policy.DiskReserveBytes < 0 || policy.PIDsReserve < 0 || policy.MaxContainers <= 0 || policy.MaxContainers > MaxConfiguredContainers ||
		profile.MemoryBytes <= 0 || profile.CPU <= 0 || math.IsNaN(profile.CPU) || math.IsInf(profile.CPU, 0) || profile.DiskBytes <= 0 || profile.PIDs <= 0 {
		return 0, ErrInvalidPolicy
	}
	mem := s.MemoryTotalBytes
	if s.CgroupMemoryMax != math.MaxInt64 && s.CgroupMemoryMax < mem {
		mem = s.CgroupMemoryMax
	}
	cpu := math.Min(s.CPUCount, s.CgroupCPUCount)
	memSlots := slotsAfterReserve(mem, policy.MemoryReserveBytes, profile.MemoryBytes)
	cpuSlots := slotsAfterReserveFloat(cpu, policy.CPUReserve, profile.CPU)
	diskSlots := slotsAfterReserve(s.DiskTotalBytes, policy.DiskReserveBytes, profile.DiskBytes)
	pidsSlots := policy.MaxContainers
	if s.PIDsMax != math.MaxInt64 {
		pidsSlots = slotsAfterReserve(s.PIDsMax, policy.PIDsReserve, profile.PIDs)
	}
	return min(memSlots, cpuSlots, diskSlots, pidsSlots, policy.MaxContainers), nil
}

func slotsAfterReserve(total, reserve, perContainer int64) int {
	if total <= reserve {
		return 0
	}
	return int((total - reserve) / perContainer)
}

func slotsAfterReserveFloat(total, reserve, perContainer float64) int {
	remaining := total - reserve
	if remaining <= 0 {
		return 0
	}
	return int(remaining / perContainer)
}

// Assess computes how many additional containers fit after preserving host reserves.
// It is deliberately conservative: unknown or exhausted measurements admit zero work.
func Assess(s Snapshot, policy Policy, profile Profile) (Capacity, error) {
	return AssessWithActive(s, policy, profile, 0)
}

// AssessWithActive returns additional container slots, subtracting the containers already
// admitted by this worker from the administrator's ceiling. Resource measurements account for
// observed usage; activeContainers separately reserves the configured per-container envelope,
// which matters especially for CPU and workspaces whose measured usage is currently low.
func AssessWithActive(s Snapshot, policy Policy, profile Profile, activeContainers int) (Capacity, error) {
	if s.CgroupsVersion != "v2" || s.MemoryAvailBytes <= 0 || s.CPUCount <= 0 || s.CgroupCPUCount <= 0 || s.DiskAvailBytes <= 0 ||
		policy.MemoryReserveBytes < 0 || policy.CPUReserve < 0 || math.IsNaN(policy.CPUReserve) || math.IsInf(policy.CPUReserve, 0) ||
		policy.DiskReserveBytes < 0 || policy.PIDsReserve < 0 || policy.MaxContainers <= 0 || policy.MaxContainers > MaxConfiguredContainers || activeContainers < 0 ||
		profile.MemoryBytes <= 0 || profile.CPU <= 0 || math.IsNaN(profile.CPU) || math.IsInf(profile.CPU, 0) ||
		profile.DiskBytes <= 0 || profile.PIDs <= 0 || s.Load1 < 0 || math.IsNaN(s.Load1) || math.IsInf(s.Load1, 0) {
		return Capacity{Reason: "CAPACITY_UNKNOWN"}, ErrInvalidPolicy
	}

	effectiveMemory := s.MemoryAvailBytes
	if s.CgroupMemoryMax != math.MaxInt64 {
		cgroupAvailable := s.CgroupMemoryMax - s.CgroupMemoryBytes
		if cgroupAvailable < effectiveMemory {
			effectiveMemory = cgroupAvailable
		}
	}
	effectiveMemory -= policy.MemoryReserveBytes

	effectiveCPU := s.CPUCount
	if s.CgroupCPUCount < effectiveCPU {
		effectiveCPU = s.CgroupCPUCount
	}
	// The host load average is a conservative pressure signal; cgroup CPU quota remains
	// the hard per-container bound applied by Podman.
	effectiveCPU -= policy.CPUReserve + s.Load1

	effectiveDisk := s.DiskAvailBytes - policy.DiskReserveBytes
	effectivePIDs := int64(math.MaxInt64)
	if s.PIDsMax != math.MaxInt64 {
		effectivePIDs = s.PIDsMax - s.PIDsCurrent - policy.PIDsReserve
	}

	if effectiveMemory < 0 {
		effectiveMemory = 0
	}
	if effectiveCPU < 0 {
		effectiveCPU = 0
	}
	if effectiveDisk < 0 {
		effectiveDisk = 0
	}
	if effectivePIDs < 0 {
		effectivePIDs = 0
	}
	byMemory := additionalSlots(int(effectiveMemory/profile.MemoryBytes), activeContainers)
	byCPU := additionalSlots(int(effectiveCPU/profile.CPU), activeContainers)
	byDisk := additionalSlots(int(effectiveDisk/profile.DiskBytes), activeContainers)
	byAdminLimit := policy.MaxContainers - activeContainers
	if byAdminLimit < 0 {
		byAdminLimit = 0
	}
	byPIDs := policy.MaxContainers
	if effectivePIDs != math.MaxInt64 {
		byPIDs = additionalSlots(int(effectivePIDs/profile.PIDs), activeContainers)
	} else {
		byPIDs = additionalSlots(byPIDs, activeContainers)
	}
	slots := min(byMemory, byCPU, byDisk, byPIDs, byAdminLimit)
	if slots < 0 {
		slots = 0
	}
	capacity := Capacity{CanStart: slots > 0, Slots: slots, MemoryFor: effectiveMemory, CPUFor: effectiveCPU, DiskFor: effectiveDisk, PIDsFor: effectivePIDs}
	if slots > 0 {
		capacity.Reason = "AVAILABLE"
		return capacity, nil
	}
	switch {
	case byMemory == 0:
		capacity.Reason = "MEMORY_RESERVE"
	case byCPU == 0:
		capacity.Reason = "CPU_RESERVE"
	case byDisk == 0:
		capacity.Reason = "DISK_RESERVE"
	case byPIDs == 0:
		capacity.Reason = "PID_RESERVE"
	default:
		capacity.Reason = "CONTAINER_LIMIT"
	}
	return capacity, nil
}

func additionalSlots(total, active int) int {
	remaining := total - active
	if remaining < 0 {
		return 0
	}
	return remaining
}
