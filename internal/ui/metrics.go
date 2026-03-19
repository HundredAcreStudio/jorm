package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProcessMetrics samples CPU and RAM for running processes.
type ProcessMetrics struct {
	mu   sync.Mutex
	pids map[string]int // agent ID → PID
}

// NewProcessMetrics creates a new ProcessMetrics.
func NewProcessMetrics() *ProcessMetrics {
	return &ProcessMetrics{
		pids: make(map[string]int),
	}
}

// RegisterPID associates an agent ID with a process ID.
func (pm *ProcessMetrics) RegisterPID(agentID string, pid int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.pids[agentID] = pid
}

// UnregisterPID removes an agent's PID.
func (pm *ProcessMetrics) UnregisterPID(agentID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.pids, agentID)
}

// SamplePID returns CPU% and RAM in MB for a given PID.
func SamplePID(pid int) (cpu float64, ramMB float64, err error) {
	switch runtime.GOOS {
	case "darwin", "linux":
		out, execErr := exec.Command("ps", "-o", "%cpu=,rss=", "-p", strconv.Itoa(pid)).Output()
		if execErr != nil {
			return 0, 0, fmt.Errorf("sampling pid %d: %w", pid, execErr)
		}
		fields := strings.Fields(strings.TrimSpace(string(out)))
		if len(fields) < 2 {
			return 0, 0, fmt.Errorf("unexpected ps output for pid %d: %q", pid, string(out))
		}
		cpu, err = strconv.ParseFloat(fields[0], 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parsing cpu for pid %d: %w", pid, err)
		}
		rssKB, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parsing rss for pid %d: %w", pid, err)
		}
		ramMB = rssKB / 1024.0
		return cpu, ramMB, nil
	default:
		return 0, 0, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// StartSampling starts a goroutine that samples all registered PIDs at the given interval.
func (pm *ProcessMetrics) StartSampling(ctx context.Context, interval time.Duration, callback func(id string, cpu, ramMB float64)) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pm.mu.Lock()
				snapshot := make(map[string]int, len(pm.pids))
				for k, v := range pm.pids {
					snapshot[k] = v
				}
				pm.mu.Unlock()

				for id, pid := range snapshot {
					cpu, ramMB, err := SamplePID(pid)
					if err != nil {
						continue // process may have exited
					}
					callback(id, cpu, ramMB)
				}
			}
		}
	}()
}
