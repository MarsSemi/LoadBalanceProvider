//go:build linux

package systemmonitor

import (
	"context"
	"os"
	"path/filepath"
)

func collectPlatformGPUUsage(_ context.Context) gpuReading {
	paths, err := filepath.Glob("/sys/class/drm/card*/device/gpu_busy_percent")
	if err != nil {
		return gpuReading{}
	}
	values := make([]float64, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if value, valid := parseGPUPercent(string(data)); valid {
			values = append(values, value)
		}
	}
	return newGPUReading("linux-amdgpu-sysfs", values)
}
