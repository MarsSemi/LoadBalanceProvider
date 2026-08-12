package systemmonitor

import (
	"context"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	gopsutilnet "github.com/shirou/gopsutil/v3/net"
)

func (m *Monitor) Details(ctx context.Context) map[string]any {
	if ctx == nil {
		ctx = context.Background()
	}
	result := map[string]any{
		"success":      true,
		"generated_at": time.Now().Format(time.RFC3339Nano),
		"warnings":     []string{},
	}
	warnings := make([]string, 0)

	if info, err := host.InfoWithContext(ctx); err != nil {
		warnings = append(warnings, "無法讀取作業系統資訊: "+err.Error())
	} else {
		bootTime := ""
		if info.BootTime > 0 {
			bootTime = time.Unix(int64(info.BootTime), 0).Format(time.RFC3339)
		}
		result["system"] = map[string]any{
			"hostname": info.Hostname, "os": info.OS, "platform": info.Platform,
			"platform_version": info.PlatformVersion, "kernel_version": info.KernelVersion,
			"kernel_arch": info.KernelArch, "virtualization_system": info.VirtualizationSystem,
			"virtualization_role": info.VirtualizationRole, "uptime_seconds": info.Uptime,
			"boot_time": bootTime, "process_count": info.Procs,
		}
	}

	result["cpu"], warnings = collectCPUDetails(ctx, warnings)
	result["memory"], warnings = collectMemoryDetails(ctx, warnings)
	gpu := collectGPUUsage()
	result["gpu"] = map[string]any{
		"available": gpu.Available, "source": gpu.Source,
		"device_count": gpu.DeviceCount, "used_percent": roundValue(gpu.Percent),
	}
	result["runtimes"] = collectRuntimeDetails(ctx)
	result["disks"], warnings = collectDiskDetails(ctx, warnings)
	result["network_interfaces"], warnings = collectNetworkDetails(ctx, warnings)
	result["temperatures"] = collectTemperatureDetails(ctx)
	result["warnings"] = warnings
	return result
}

func collectCPUDetails(ctx context.Context, warnings []string) (map[string]any, []string) {
	logical, logicalErr := cpu.CountsWithContext(ctx, true)
	physical, physicalErr := cpu.CountsWithContext(ctx, false)
	infos, infoErr := cpu.InfoWithContext(ctx)
	if logicalErr != nil {
		warnings = append(warnings, "無法讀取 Logical CPU 數量: "+logicalErr.Error())
	}
	if physicalErr != nil {
		warnings = append(warnings, "無法讀取 Physical CPU 數量: "+physicalErr.Error())
	}
	if infoErr != nil {
		warnings = append(warnings, "無法讀取 CPU 型號: "+infoErr.Error())
		infos = nil
	}

	type cpuPackage struct {
		VendorID          string   `json:"vendor_id"`
		ModelName         string   `json:"model_name"`
		PhysicalID        string   `json:"physical_id"`
		Cores             int32    `json:"cores"`
		LogicalProcessors int      `json:"logical_processors"`
		MHz               float64  `json:"mhz"`
		CacheSizeKB       int32    `json:"cache_size_kb"`
		Flags             []string `json:"flags"`
	}
	byKey := map[string]*cpuPackage{}
	order := make([]string, 0)
	for _, info := range infos {
		key := strings.TrimSpace(info.PhysicalID)
		if key == "" {
			key = strings.Join([]string{info.VendorID, info.ModelName, info.Family, info.Model}, "|")
		}
		current := byKey[key]
		if current == nil {
			flags := append([]string(nil), info.Flags...)
			sort.Strings(flags)
			current = &cpuPackage{
				VendorID: info.VendorID, ModelName: info.ModelName, PhysicalID: info.PhysicalID,
				Cores: info.Cores, MHz: roundValue(info.Mhz), CacheSizeKB: info.CacheSize, Flags: flags,
			}
			byKey[key] = current
			order = append(order, key)
		}
		current.LogicalProcessors++
		if info.Cores > current.Cores {
			current.Cores = info.Cores
		}
		if info.Mhz > current.MHz {
			current.MHz = roundValue(info.Mhz)
		}
	}
	packages := make([]cpuPackage, 0, len(order))
	for _, key := range order {
		packages = append(packages, *byKey[key])
	}
	if len(packages) == 1 && packages[0].LogicalProcessors < logical {
		packages[0].LogicalProcessors = logical
	}
	modelName := ""
	vendorID := ""
	mhz := 0.0
	if len(packages) > 0 {
		modelName = packages[0].ModelName
		vendorID = packages[0].VendorID
		mhz = packages[0].MHz
	}
	return map[string]any{
		"model_name": modelName, "vendor_id": vendorID, "mhz": mhz,
		"logical_cores": logical, "physical_cores": physical, "packages": packages,
	}, warnings
}

func collectMemoryDetails(ctx context.Context, warnings []string) (map[string]any, []string) {
	memory, memoryErr := mem.VirtualMemoryWithContext(ctx)
	swap, swapErr := mem.SwapMemoryWithContext(ctx)
	if memoryErr != nil {
		warnings = append(warnings, "無法讀取記憶體資訊: "+memoryErr.Error())
		memory = &mem.VirtualMemoryStat{}
	}
	if swapErr != nil {
		warnings = append(warnings, "無法讀取 Swap 資訊: "+swapErr.Error())
		swap = &mem.SwapMemoryStat{}
	}
	return map[string]any{
		"total_bytes": memory.Total, "available_bytes": memory.Available,
		"used_bytes": memory.Used, "used_percent": roundValue(memory.UsedPercent),
		"free_bytes": memory.Free, "cached_bytes": memory.Cached,
		"swap_total_bytes": swap.Total, "swap_used_bytes": swap.Used,
		"swap_free_bytes": swap.Free, "swap_used_percent": roundValue(swap.UsedPercent),
	}, warnings
}

type runtimeDetail struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Command   string `json:"command,omitempty"`
}

func collectRuntimeDetails(ctx context.Context) []runtimeDetail {
	details := []runtimeDetail{
		{ID: "go", Name: "Go", Available: true, Version: runtime.Version(), Command: runtime.GOOS + "/" + runtime.GOARCH},
		{ID: "java", Name: "Java"}, {ID: "python", Name: "Python"}, {ID: "nodejs", Name: "Node.js"},
	}
	probes := []struct {
		index    int
		commands [][]string
	}{
		{1, [][]string{{"java", "-version"}}},
		{2, [][]string{{"python3", "--version"}, {"python", "--version"}}},
		{3, [][]string{{"node", "--version"}, {"nodejs", "--version"}}},
	}
	var waitGroup sync.WaitGroup
	for _, probe := range probes {
		probe := probe
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			details[probe.index] = probeRuntime(ctx, details[probe.index], probe.commands)
		}()
	}
	waitGroup.Wait()
	return details
}

func probeRuntime(parent context.Context, detail runtimeDetail, commands [][]string) runtimeDetail {
	for _, command := range commands {
		path, err := exec.LookPath(command[0])
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(parent, 2*time.Second)
		output, commandErr := exec.CommandContext(ctx, path, command[1:]...).CombinedOutput()
		cancel()
		if commandErr != nil && len(output) == 0 {
			continue
		}
		version := normalizeRuntimeVersion(string(output))
		if version != "" {
			detail.Available = true
			detail.Version = version
			detail.Command = path
			return detail
		}
	}
	return detail
}

func normalizeRuntimeVersion(value string) string {
	lines := strings.FieldsFunc(strings.TrimSpace(value), func(character rune) bool {
		return character == '\r' || character == '\n'
	})
	for index := range lines {
		lines[index] = strings.Join(strings.Fields(lines[index]), " ")
	}
	if len(lines) > 2 {
		lines = lines[:2]
	}
	return strings.Join(lines, " · ")
}

func collectDiskDetails(ctx context.Context, warnings []string) ([]map[string]any, []string) {
	partitions, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return []map[string]any{}, append(warnings, "無法讀取磁碟分割區: "+err.Error())
	}
	sort.Slice(partitions, func(left, right int) bool { return partitions[left].Mountpoint < partitions[right].Mountpoint })
	result := make([]map[string]any, 0, len(partitions))
	seen := map[string]bool{}
	for _, partition := range partitions {
		key := partition.Device + "\x00" + partition.Mountpoint
		if seen[key] {
			continue
		}
		seen[key] = true
		entry := map[string]any{
			"device": partition.Device, "mountpoint": partition.Mountpoint,
			"filesystem": partition.Fstype, "options": partition.Opts, "available": false,
		}
		if usage, usageErr := disk.UsageWithContext(ctx, partition.Mountpoint); usageErr == nil {
			entry["available"] = true
			entry["total_bytes"] = usage.Total
			entry["used_bytes"] = usage.Used
			entry["free_bytes"] = usage.Free
			entry["used_percent"] = roundValue(usage.UsedPercent)
		} else {
			entry["error"] = usageErr.Error()
		}
		result = append(result, entry)
	}
	return result, warnings
}

func collectNetworkDetails(ctx context.Context, warnings []string) ([]map[string]any, []string) {
	interfaces, err := gopsutilnet.InterfacesWithContext(ctx)
	if err != nil {
		return []map[string]any{}, append(warnings, "無法讀取網路介面: "+err.Error())
	}
	sort.Slice(interfaces, func(left, right int) bool { return interfaces[left].Name < interfaces[right].Name })
	result := make([]map[string]any, 0, len(interfaces))
	for _, networkInterface := range interfaces {
		addresses := make([]string, 0, len(networkInterface.Addrs))
		for _, address := range networkInterface.Addrs {
			addresses = append(addresses, address.Addr)
		}
		result = append(result, map[string]any{
			"index": networkInterface.Index, "name": networkInterface.Name, "mtu": networkInterface.MTU,
			"hardware_address": networkInterface.HardwareAddr, "flags": networkInterface.Flags, "addresses": addresses,
		})
	}
	return result, warnings
}

func collectTemperatureDetails(ctx context.Context) []map[string]any {
	sensors, err := host.SensorsTemperaturesWithContext(ctx)
	if err != nil {
		return []map[string]any{}
	}
	sort.Slice(sensors, func(left, right int) bool { return sensors[left].SensorKey < sensors[right].SensorKey })
	result := make([]map[string]any, 0, len(sensors))
	for _, sensor := range sensors {
		result = append(result, map[string]any{
			"name": sensor.SensorKey, "temperature": roundValue(sensor.Temperature),
			"high": roundValue(sensor.High), "critical": roundValue(sensor.Critical),
		})
	}
	return result
}
