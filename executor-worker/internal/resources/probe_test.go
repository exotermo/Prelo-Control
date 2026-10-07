package resources

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProbeReadsCgroupsV2AndDedicatedStorage(t *testing.T) {
	root := t.TempDir()
	proc := filepath.Join(root, "proc")
	cgroups := filepath.Join(root, "cgroup")
	storage := filepath.Join(root, "storage")
	for _, dir := range []string{filepath.Join(proc, "self"), filepath.Join(cgroups, "user.slice", "prelo"), storage} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(cgroups, "cgroup.controllers"), "cpu memory pids\n")
	write(filepath.Join(proc, "self", "cgroup"), "0::/user.slice/prelo\n")
	write(filepath.Join(proc, "meminfo"), "MemTotal:       4000000 kB\nMemAvailable:   2000000 kB\n")
	write(filepath.Join(proc, "loadavg"), "0.25 0.40 0.38 1/128 12345\n")
	group := filepath.Join(cgroups, "user.slice", "prelo")
	write(filepath.Join(group, "memory.max"), "2147483648\n")
	write(filepath.Join(group, "memory.current"), "536870912\n")
	write(filepath.Join(group, "cpu.max"), "150000 100000\n")
	write(filepath.Join(group, "cpuset.cpus.effective"), "0-1\n")
	write(filepath.Join(group, "pids.max"), "512\n")
	write(filepath.Join(group, "pids.current"), "32\n")

	p := &Probe{ProcRoot: proc, CgroupRoot: cgroups, StorageDir: storage}
	s, err := p.Read()
	if err != nil {
		t.Fatal(err)
	}
	if s.CgroupsVersion != "v2" || s.MemoryTotalBytes != 4000000*1024 || s.MemoryAvailBytes != 2000000*1024 ||
		s.CgroupMemoryMax != 2147483648 || s.CgroupMemoryBytes != 536870912 || s.CgroupCPUCount != 1.5 || s.CPUCount != 2 ||
		s.Load1 != .25 || s.PIDsMax != 512 || s.PIDsCurrent != 32 || s.DiskAvailBytes <= 0 {
		t.Fatalf("unexpected resource snapshot: %+v", s)
	}
}

func TestProbeRejectsMissingCgroupsV2AndStorage(t *testing.T) {
	if _, err := NewProbe("relative/path"); err == nil {
		t.Fatal("accepted relative storage path")
	}
	root := t.TempDir()
	storage := filepath.Join(root, "storage")
	if err := os.Mkdir(storage, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := NewProbe(storage)
	if err != nil {
		t.Fatal(err)
	}
	p.ProcRoot = root
	p.CgroupRoot = filepath.Join(root, "cgroup-v1")
	if _, err := p.Read(); err != ErrCgroupsV2Required {
		t.Fatalf("expected cgroups v2 requirement, got %v", err)
	}
}
