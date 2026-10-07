package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/exotermo/prelo-executor-worker/internal/resources"
	containerruntime "github.com/exotermo/prelo-executor-worker/internal/runtime"
)

// Both server and VM worker default to heartbeat-only. Runtime is opt-in after VM validation.
func main() {
	client, err := newPreloClient(os.Getenv("PRELO_BASE_URL"), os.Getenv("PRELO_EXECUTOR_TOKEN"), false)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var podman *containerruntime.Podman
	var resourceProbe *resources.Probe
	var resourceConfig resources.Config
	imageDigest := ""
	if os.Getenv("PRELO_WORKER_ENABLE_RUNTIME") == "true" {
		if os.Geteuid() == 0 {
			log.Fatal("workspace runtime must run as an unprivileged user")
		}
		image := os.Getenv("PRELO_WORKER_IMAGE")
		if !strings.Contains(image, "@sha256:") {
			log.Fatal("PRELO_WORKER_IMAGE must be pinned by digest")
		}
		resourceConfig, err = resources.LoadConfig()
		if err != nil {
			log.Fatalf("invalid worker resource configuration: %v", err)
		}
		resourceProbe, err = resources.NewProbe(resourceConfig.StorageDir)
		if err != nil {
			log.Fatalf("worker storage is unavailable: %v", err)
		}
		initialSnapshot, probeErr := resourceProbe.Read()
		if probeErr != nil {
			log.Fatalf("resource probe failed; refusing to enable runtime: %v", probeErr)
		}
		podman, err = containerruntime.NewPodmanWithConfig(containerruntime.ExecRunner{}, image,
			filepath.Join(resourceConfig.StorageDir, "podman"), resourceConfig.Profile)
		if err != nil {
			log.Fatal(err)
		}
		activeExecutions, activeErr := podman.ActiveExecutions(ctx)
		if activeErr != nil {
			log.Fatalf("cannot enumerate worker containers: %v", activeErr)
		}
		initialCapacity, assessErr := resources.AssessWithActive(initialSnapshot, resourceConfig.Policy, resourceConfig.Profile, len(activeExecutions))
		if assessErr != nil {
			log.Fatalf("resource assessment failed; refusing to enable runtime: %v", assessErr)
		}
		maxSlots, maxErr := resources.MaximumSlots(initialSnapshot, resourceConfig.Policy, resourceConfig.Profile)
		if maxErr != nil {
			log.Fatalf("maximum capacity assessment failed; refusing to enable runtime: %v", maxErr)
		}
		log.Printf("resource probe ready cgroups=%s mem_available_bytes=%d cpu_quota=%.2f disk_available_bytes=%d active_containers=%d additional_slots=%d maximum_slots=%d capacity=%s",
			initialSnapshot.CgroupsVersion, initialSnapshot.MemoryAvailBytes, initialSnapshot.CgroupCPUCount,
			initialSnapshot.DiskAvailBytes, len(activeExecutions), initialCapacity.Slots, maxSlots, initialCapacity.Reason)
		imageDigest = digestFromImage(image)
	}
	for {
		result, err := client.heartbeat(ctx)
		if errors.Is(err, errRevoked) {
			log.Fatal("executor credential was revoked")
		}
		if err != nil {
			log.Printf("heartbeat failed: %v", err)
		} else {
			log.Printf("worker=%s project=%s status=%s", result.WorkerID, result.ProjectID, result.Status)
			if result.CanExecute && podman != nil {
				snapshot, probeErr := resourceProbe.Read()
				if probeErr != nil {
					log.Printf("resource probe failed; not claiming job: %v", probeErr)
				} else {
					activeExecutions, activeErr := podman.ActiveExecutions(ctx)
					if activeErr != nil {
						log.Printf("cannot enumerate worker containers; not claiming job: %v", activeErr)
					} else {
						capacity, assessErr := resources.AssessWithActive(snapshot, resourceConfig.Policy, resourceConfig.Profile, len(activeExecutions))
						maxSlots, maxErr := resources.MaximumSlots(snapshot, resourceConfig.Policy, resourceConfig.Profile)
						if assessErr != nil || maxErr != nil {
							log.Printf("resource assessment failed; not claiming job: current=%v maximum=%v", assessErr, maxErr)
						} else {
							log.Printf("worker capacity active_containers=%d additional_slots=%d maximum_slots=%d state=%s",
								len(activeExecutions), capacity.Slots, maxSlots, capacity.Reason)
							profile := resourceConfig.Profile
							job, claimErr := client.claim(ctx, executorClaimCapacity{
								ProfileID: resources.WorkspaceSmallProfileID, MemoryBytes: profile.MemoryBytes,
								CPUQuotaMilli: int64(profile.CPU * 1000), DiskBytes: profile.DiskBytes, PIDs: profile.PIDs,
								AvailableSlots: capacity.Slots, MaximumSlots: maxSlots,
								ActiveContainers: len(activeExecutions), ActiveExecutionIDs: activeExecutions, ObservedAt: time.Now().UTC(),
							})
							if errors.Is(claimErr, errRevoked) {
								log.Fatal("executor credential was revoked")
							}
							if claimErr != nil {
								log.Printf("claim failed: %v", claimErr)
							} else if job != nil {
								if runErr := runClaimedJob(ctx, client, podman, result.WorkerID, result.ProjectID, imageDigest, *job); runErr != nil {
									log.Printf("job %s failed: %v", job.JobID, runErr)
								}
							}
						}
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}
