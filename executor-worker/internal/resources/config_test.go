package resources

import "testing"

func TestLoadConfigUsesConservativeInitialProfile(t *testing.T) {
	for _, key := range []string{
		"PRELO_WORKER_DATA_DIR", "PRELO_WORKER_MEMORY_RESERVE_BYTES", "PRELO_WORKER_CPU_RESERVE",
		"PRELO_WORKER_DISK_RESERVE_BYTES", "PRELO_WORKER_PIDS_RESERVE", "PRELO_WORKER_MAX_CONTAINERS",
		"PRELO_WORKER_CONTAINER_MEMORY_BYTES", "PRELO_WORKER_CONTAINER_CPUS",
		"PRELO_WORKER_CONTAINER_DISK_BYTES", "PRELO_WORKER_CONTAINER_PIDS",
	} {
		t.Setenv(key, "")
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StorageDir != "/mnt/storage/prelo-executor" || cfg.Policy.MaxContainers != 1 ||
		cfg.Policy.MemoryReserveBytes != 1536<<20 || cfg.Policy.CPUReserve != 1 ||
		cfg.Profile.MemoryBytes != 256<<20 || cfg.Profile.CPU != .5 || cfg.Profile.PIDs != 64 {
		t.Fatalf("unexpected safe defaults: %+v", cfg)
	}
}

func TestLoadConfigRejectsInvalidLimits(t *testing.T) {
	t.Setenv("PRELO_WORKER_MAX_CONTAINERS", "0")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("accepted zero container concurrency")
	}
	t.Setenv("PRELO_WORKER_MAX_CONTAINERS", "1")
	t.Setenv("PRELO_WORKER_CONTAINER_MEMORY_BYTES", "not-a-number")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("accepted malformed memory limit")
	}
	t.Setenv("PRELO_WORKER_CONTAINER_MEMORY_BYTES", "")
	t.Setenv("PRELO_WORKER_MAX_CONTAINERS", "65")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("accepted a concurrency ceiling above the hard safety cap")
	}
}
