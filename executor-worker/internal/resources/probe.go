package resources

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

var ErrCgroupsV2Required = errors.New("cgroups v2 is required for resource-aware execution")
var ErrInvalidStoragePath = errors.New("executor storage path must be an existing writable directory")

// Snapshot contains host and delegated-cgroup capacity. Zero means a metric is unavailable,
// never unlimited, so callers can fail closed when a required measurement is missing.
type Snapshot struct {
	CgroupsVersion    string
	MemoryTotalBytes  int64
	MemoryAvailBytes  int64
	CgroupMemoryMax   int64
	CgroupMemoryBytes int64
	CPUCount          float64
	CgroupCPUCount    float64
	Load1             float64
	PIDsMax           int64
	PIDsCurrent       int64
	DiskTotalBytes    int64
	DiskAvailBytes    int64
}

type Probe struct {
	ProcRoot   string
	CgroupRoot string
	StorageDir string
}

func NewProbe(storageDir string) (*Probe, error) {
	if !filepath.IsAbs(storageDir) {
		return nil, ErrInvalidStoragePath
	}
	if err := os.MkdirAll(storageDir, 0700); err != nil {
		return nil, ErrInvalidStoragePath
	}
	info, err := os.Stat(storageDir)
	if err != nil || !info.IsDir() {
		return nil, ErrInvalidStoragePath
	}
	file, err := os.CreateTemp(storageDir, ".prelo-write-check-*")
	if err != nil {
		return nil, ErrInvalidStoragePath
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return nil, ErrInvalidStoragePath
	}
	if err := os.Remove(name); err != nil {
		return nil, ErrInvalidStoragePath
	}
	return &Probe{ProcRoot: "/proc", CgroupRoot: "/sys/fs/cgroup", StorageDir: storageDir}, nil
}

// Read samples the current process's delegated cgroup as well as host memory and storage.
// It supports cgroups v2 only; the worker must not assume limits were applied when they were not.
func (p *Probe) Read() (Snapshot, error) {
	if p == nil || p.ProcRoot == "" || p.CgroupRoot == "" || p.StorageDir == "" {
		return Snapshot{}, ErrInvalidStoragePath
	}
	if _, err := os.Stat(filepath.Join(p.CgroupRoot, "cgroup.controllers")); err != nil {
		return Snapshot{}, ErrCgroupsV2Required
	}
	mem, err := parseMemInfo(filepath.Join(p.ProcRoot, "meminfo"))
	if err != nil {
		return Snapshot{}, err
	}
	cgroupPath, err := processCgroupPath(filepath.Join(p.ProcRoot, "self", "cgroup"))
	if err != nil {
		return Snapshot{}, err
	}
	cgroupDir := filepath.Join(p.CgroupRoot, cgroupPath)
	if !pathWithin(p.CgroupRoot, cgroupDir) {
		return Snapshot{}, ErrCgroupsV2Required
	}
	memMax, err := readLimit(filepath.Join(cgroupDir, "memory.max"))
	if err != nil {
		return Snapshot{}, err
	}
	memCurrent, err := readUint(filepath.Join(cgroupDir, "memory.current"))
	if err != nil {
		return Snapshot{}, err
	}
	cpuCount, err := readCPUQuota(filepath.Join(cgroupDir, "cpu.max"))
	if err != nil {
		return Snapshot{}, err
	}
	allowedCPUs, cpusetErr := readCPUSetCount(filepath.Join(cgroupDir, "cpuset.cpus.effective"))
	if cpusetErr != nil && !errors.Is(cpusetErr, os.ErrNotExist) {
		return Snapshot{}, cpusetErr
	}
	hostCPUs := runtime.NumCPU()
	if allowedCPUs > 0 && allowedCPUs < hostCPUs {
		hostCPUs = allowedCPUs
	}
	load1, err := readLoad1(filepath.Join(p.ProcRoot, "loadavg"))
	if err != nil {
		return Snapshot{}, err
	}
	pidsMax, err := readLimit(filepath.Join(cgroupDir, "pids.max"))
	if err != nil {
		return Snapshot{}, err
	}
	pidsCurrent, err := readUint(filepath.Join(cgroupDir, "pids.current"))
	if err != nil {
		return Snapshot{}, err
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(p.StorageDir, &stat); err != nil {
		return Snapshot{}, err
	}
	diskTotal := int64(stat.Blocks) * int64(stat.Bsize)
	diskAvail := int64(stat.Bavail) * int64(stat.Bsize)

	return Snapshot{
		CgroupsVersion: "v2", MemoryTotalBytes: mem.total, MemoryAvailBytes: mem.available,
		CgroupMemoryMax: memMax, CgroupMemoryBytes: memCurrent,
		CPUCount: float64(hostCPUs), CgroupCPUCount: cpuCount, Load1: load1,
		PIDsMax: pidsMax, PIDsCurrent: pidsCurrent, DiskTotalBytes: diskTotal, DiskAvailBytes: diskAvail,
	}, nil
}

func readLoad1(path string) (float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 1 {
		return 0, errors.New("invalid proc loadavg")
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || load < 0 {
		return 0, errors.New("invalid proc loadavg")
	}
	return load, nil
}

type memoryInfo struct{ total, available int64 }

func parseMemInfo(path string) (memoryInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return memoryInfo{}, err
	}
	defer f.Close()
	var out memoryInfo
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 2 {
			continue
		}
		value, parseErr := strconv.ParseInt(fields[1], 10, 64)
		if parseErr != nil || value < 0 {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			out.total = value * 1024
		case "MemAvailable":
			out.available = value * 1024
		}
	}
	if err := s.Err(); err != nil {
		return memoryInfo{}, err
	}
	if out.total == 0 || out.available == 0 {
		return memoryInfo{}, errors.New("MemTotal or MemAvailable missing from proc meminfo")
	}
	return out, nil
}

func processCgroupPath(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.SplitN(line, ":", 3)
		if len(fields) == 3 && fields[0] == "0" && fields[1] == "" {
			clean := filepath.Clean("/" + fields[2])
			if clean == "/" {
				return ".", nil
			}
			return strings.TrimPrefix(clean, string(filepath.Separator)), nil
		}
	}
	return "", ErrCgroupsV2Required
}

func pathWithin(root, candidate string) bool {
	r, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	c, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(r, c)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// readLimit returns math.MaxInt64 for the cgroups v2 value "max".
func readLimit(path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	value := strings.TrimSpace(string(b))
	if value == "max" {
		return math.MaxInt64, nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid cgroup limit in %s", filepath.Base(path))
	}
	return n, nil
}

func readUint(path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid cgroup counter in %s", filepath.Base(path))
	}
	return n, nil
}

func readCPUQuota(path string) (float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(b))
	if len(fields) != 2 {
		return 0, errors.New("invalid cgroup cpu.max")
	}
	period, err := strconv.ParseFloat(fields[1], 64)
	if err != nil || period <= 0 {
		return 0, errors.New("invalid cgroup cpu period")
	}
	if fields[0] == "max" {
		return float64(runtime.NumCPU()), nil
	}
	quota, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || quota <= 0 {
		return 0, errors.New("invalid cgroup cpu quota")
	}
	return quota / period, nil
}

func readCPUSetCount(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	raw := strings.TrimSpace(string(b))
	if raw == "" {
		return 0, nil
	}
	count := 0
	for _, part := range strings.Split(raw, ",") {
		bounds := strings.SplitN(part, "-", 2)
		start, err := strconv.Atoi(bounds[0])
		if err != nil || start < 0 {
			return 0, errors.New("invalid cgroup cpuset")
		}
		end := start
		if len(bounds) == 2 {
			end, err = strconv.Atoi(bounds[1])
			if err != nil || end < start {
				return 0, errors.New("invalid cgroup cpuset")
			}
		}
		count += end - start + 1
	}
	return count, nil
}
