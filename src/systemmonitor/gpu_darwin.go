//go:build darwin

package systemmonitor

import (
	"context"
	"regexp"
)

var macOSGPUUtilizationPattern = regexp.MustCompile(`"Device Utilization %"\s*=\s*([0-9]+(?:\.[0-9]+)?)`)

func collectPlatformGPUUsage(ctx context.Context) gpuReading {
	for _, className := range []string{"AGXAccelerator", "IOAccelerator"} {
		output, ok := runGPUCommand(ctx, []string{"/usr/sbin/ioreg", "ioreg"}, "-r", "-d", "1", "-c", className)
		if !ok {
			continue
		}
		matches := macOSGPUUtilizationPattern.FindAllStringSubmatch(string(output), -1)
		values := make([]float64, 0, len(matches))
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			if value, valid := parseGPUPercent(match[1]); valid {
				values = append(values, value)
			}
		}
		if reading := newGPUReading("macos-ioreg", values); reading.Available {
			return reading
		}
	}
	return gpuReading{}
}
