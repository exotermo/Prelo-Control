package resources

import (
	"fmt"
	"os"
	"strconv"
)

const WorkspaceSmallProfileID = "workspace-small-v1"
const MaxConfiguredContainers = 64

type Config struct {
	StorageDir string
	Policy     Policy
	Profile    Profile
}

// LoadConfig uses conservative defaults for the initial low-memory host. Operators may raise
// MaxContainers after observing real usage; resource limits inside each container remain hard.
func LoadConfig() (Config, error) {
	storageDir := os.Getenv("PRELO_WORKER_DATA_DIR")
	if storageDir == "" {
		storageDir = "/mnt/storage/prelo-executor"
	}
	memoryReserve, err := envInt64("PRELO_WORKER_MEMORY_RESERVE_BYTES", 1536<<20)
	if err != nil {
		return Config{}, err
	}
	cpuReserve, err := envFloat("PRELO_WORKER_CPU_RESERVE", 1)
	if err != nil {
		return Config{}, err
	}
	diskReserve, err := envInt64("PRELO_WORKER_DISK_RESERVE_BYTES", 5<<30)
	if err != nil {
		return Config{}, err
	}
	pidsReserve, err := envInt64("PRELO_WORKER_PIDS_RESERVE", 128)
	if err != nil {
		return Config{}, err
	}
	maxContainers, err := envInt("PRELO_WORKER_MAX_CONTAINERS", 1)
	if err != nil {
		return Config{}, err
	}
	memory, err := envInt64("PRELO_WORKER_CONTAINER_MEMORY_BYTES", 256<<20)
	if err != nil {
		return Config{}, err
	}
	cpu, err := envFloat("PRELO_WORKER_CONTAINER_CPUS", .5)
	if err != nil {
		return Config{}, err
	}
	disk, err := envInt64("PRELO_WORKER_CONTAINER_DISK_BYTES", 1<<30)
	if err != nil {
		return Config{}, err
	}
	pids, err := envInt64("PRELO_WORKER_CONTAINER_PIDS", 64)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		StorageDir: storageDir,
		Policy: Policy{MemoryReserveBytes: memoryReserve, CPUReserve: cpuReserve,
			DiskReserveBytes: diskReserve, PIDsReserve: pidsReserve, MaxContainers: maxContainers},
		Profile: Profile{MemoryBytes: memory, CPU: cpu, DiskBytes: disk, PIDs: pids},
	}
	if !cfg.valid() {
		return Config{}, ErrInvalidPolicy
	}
	return cfg, nil
}

func (c Config) valid() bool {
	return c.StorageDir != "" && c.Policy.MemoryReserveBytes >= 0 && c.Policy.CPUReserve >= 0 &&
		c.Policy.DiskReserveBytes >= 0 && c.Policy.PIDsReserve >= 0 && c.Policy.MaxContainers > 0 &&
		c.Policy.MaxContainers <= MaxConfiguredContainers && c.Profile.MemoryBytes > 0 && c.Profile.CPU > 0 && c.Profile.DiskBytes > 0 && c.Profile.PIDs > 0
}

func envInt64(name string, fallback int64) (int64, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer number of bytes/counts", name)
	}
	return n, nil
}

func envInt(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return n, nil
}

func envFloat(name string, fallback float64) (float64, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", name)
	}
	return n, nil
}
