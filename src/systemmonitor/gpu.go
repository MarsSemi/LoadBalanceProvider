package systemmonitor

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const gpuProbeTimeout = 3 * time.Second

type gpuReading struct {
	Percent     float64
	Available   bool
	Source      string
	DeviceCount int
}

func collectGPUUsage() gpuReading {
	ctx, cancel := context.WithTimeout(context.Background(), gpuProbeTimeout)
	defer cancel()
	if reading := collectNVIDIAGPUUsage(ctx); reading.Available {
		return reading
	}
	return collectPlatformGPUUsage(ctx)
}

func collectNVIDIAGPUUsage(ctx context.Context) gpuReading {
	output, ok := runGPUCommand(ctx, []string{
		"nvidia-smi",
		"/usr/bin/nvidia-smi",
		"/usr/local/bin/nvidia-smi",
	}, "--query-gpu=utilization.gpu", "--format=csv,noheader,nounits")
	if !ok {
		return gpuReading{}
	}
	values := make([]float64, 0)
	for _, line := range strings.Split(string(output), "\n") {
		if value, valid := parseGPUPercent(line); valid {
			values = append(values, value)
		}
	}
	return newGPUReading("nvidia-smi", values)
}

func newGPUReading(source string, values []float64) gpuReading {
	if len(values) == 0 {
		return gpuReading{}
	}
	maximum := 0.0
	for _, value := range values {
		maximum = math.Max(maximum, value)
	}
	return gpuReading{
		Percent:     roundValue(clampPercent(maximum)),
		Available:   true,
		Source:      strings.TrimSpace(source),
		DeviceCount: len(values),
	}
}

func parseGPUPercent(value string) (float64, bool) {
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "%"))
	if value == "" || strings.EqualFold(value, "N/A") || strings.EqualFold(value, "NA") {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(strings.ReplaceAll(value, ",", "."), 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, false
	}
	return clampPercent(parsed), true
}

func runGPUCommand(ctx context.Context, candidates []string, args ...string) ([]byte, bool) {
	path := findGPUCommand(candidates...)
	if path == "" {
		return nil, false
	}
	output, err := exec.CommandContext(ctx, path, args...).Output()
	return output, err == nil && ctx.Err() == nil
}

func findGPUCommand(candidates ...string) string {
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		if filepath.IsAbs(candidate) {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	return ""
}
