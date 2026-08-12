//go:build !darwin && !linux

package systemmonitor

import "context"

func collectPlatformGPUUsage(_ context.Context) gpuReading {
	return gpuReading{}
}
