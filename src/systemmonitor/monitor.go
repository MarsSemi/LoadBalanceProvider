package systemmonitor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	gopsutilnet "github.com/shirou/gopsutil/v3/net"
)

const (
	SampleInterval = 10 * time.Second
	RetentionDays  = 90
	bucketDuration = 5 * time.Minute
	bucketCount    = 24 * 60 / 5
	maxQueryBytes  = int64(64 * 1024 * 1024)
)

type Sample struct {
	Timestamp         string  `json:"timestamp"`
	CPUPercent        float64 `json:"cpu_percent"`
	GPUUsedPercent    float64 `json:"gpu_used_percent"`
	GPUAvailable      bool    `json:"gpu_available"`
	GPUSource         string  `json:"gpu_source,omitempty"`
	GPUDeviceCount    int     `json:"gpu_device_count,omitempty"`
	MemoryUsedPercent float64 `json:"memory_used_percent"`
	MemoryUsedBytes   uint64  `json:"memory_used_bytes"`
	MemoryTotalBytes  uint64  `json:"memory_total_bytes"`
	DiskUsedPercent   float64 `json:"disk_used_percent"`
	DiskUsedBytes     uint64  `json:"disk_used_bytes"`
	DiskTotalBytes    uint64  `json:"disk_total_bytes"`
	NetRXBytesPerSec  float64 `json:"net_rx_bytes_per_sec"`
	NetTXBytesPerSec  float64 `json:"net_tx_bytes_per_sec"`
	NetRXTotalBytes   uint64  `json:"net_rx_total_bytes"`
	NetTXTotalBytes   uint64  `json:"net_tx_total_bytes"`
}

type Point struct {
	Timestamp         string  `json:"timestamp"`
	TimeLabel         string  `json:"time"`
	SampleCount       int     `json:"sample_count"`
	CPUPercent        float64 `json:"cpu_percent"`
	GPUUsedPercent    float64 `json:"gpu_used_percent"`
	GPUAvailable      bool    `json:"gpu_available"`
	GPUSampleCount    int     `json:"gpu_sample_count"`
	MemoryUsedPercent float64 `json:"memory_used_percent"`
	DiskUsedPercent   float64 `json:"disk_used_percent"`
	NetRXBytesPerSec  float64 `json:"net_rx_bytes_per_sec"`
	NetTXBytesPerSec  float64 `json:"net_tx_bytes_per_sec"`
}

type UsageResponse struct {
	Success               bool     `json:"success"`
	Date                  string   `json:"date"`
	RangeStart            string   `json:"range_start"`
	RangeEnd              string   `json:"range_end"`
	ViewMode              string   `json:"view_mode"`
	Unit                  string   `json:"unit"`
	Dates                 []string `json:"dates"`
	Points                []Point  `json:"points"`
	Latest                *Sample  `json:"latest"`
	RawSampleCount        int      `json:"raw_sample_count"`
	InvalidSampleCount    int      `json:"invalid_sample_count"`
	Truncated             bool     `json:"truncated"`
	SampleIntervalSeconds int      `json:"sample_interval_seconds"`
	BucketSeconds         int      `json:"bucket_seconds"`
	BucketCount           int      `json:"bucket_count"`
	RetentionDays         int      `json:"retention_days"`
}

type accumulator struct {
	start    time.Time
	count    int
	cpu      float64
	gpuCount int
	gpu      float64
	memory   float64
	disk     float64
	netRX    float64
	netTX    float64
}

type Monitor struct {
	root             string
	dataDir          string
	fileMu           sync.RWMutex
	previousCPUTotal float64
	previousCPUIdle  float64
	hasPreviousCPU   bool
	previousNetRX    uint64
	previousNetTX    uint64
	previousNetAt    time.Time
	hasPreviousNet   bool
	lastErrorLogAt   time.Time
	lastPruneDate    string
}

func New(root string) *Monitor {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err == nil {
		root = absRoot
	}
	return &Monitor{
		root:    root,
		dataDir: filepath.Join(root, "data", "system", "resource_usage"),
	}
}

func (m *Monitor) Start(ctx context.Context) {
	if m == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		m.collectAndStore(time.Now())
		ticker := time.NewTicker(SampleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case sampledAt := <-ticker.C:
				m.collectAndStore(sampledAt)
			}
		}
	}()
}

func (m *Monitor) collectAndStore(sampledAt time.Time) {
	date := sampledAt.Format("2006-01-02")
	if m.lastPruneDate != date {
		m.lastPruneDate = date
		if _, err := m.prune(sampledAt); err != nil {
			m.reportError(err)
		}
	}
	sample, err := m.collect(sampledAt)
	if err != nil {
		m.reportError(err)
		return
	}
	if err := m.append(sampledAt, sample); err != nil {
		m.reportError(err)
	}
}

func (m *Monitor) collect(sampledAt time.Time) (Sample, error) {
	cpuTimes, err := cpu.Times(false)
	if err != nil || len(cpuTimes) == 0 {
		return Sample{}, fmt.Errorf("讀取 CPU 使用量失敗: %w", firstError(err, "CPU counter unavailable"))
	}
	memory, err := mem.VirtualMemory()
	if err != nil {
		return Sample{}, fmt.Errorf("讀取記憶體使用量失敗: %w", err)
	}
	diskUsage, err := disk.Usage(m.root)
	if err != nil {
		return Sample{}, fmt.Errorf("讀取磁碟使用量失敗: %w", err)
	}
	networkCounters, err := gopsutilnet.IOCounters(false)
	if err != nil || len(networkCounters) == 0 {
		return Sample{}, fmt.Errorf("讀取網路傳輸量失敗: %w", firstError(err, "network counter unavailable"))
	}

	cpuTotal, cpuIdle := cpuTotals(cpuTimes[0])
	cpuPercent := 0.0
	if m.hasPreviousCPU {
		totalDelta := cpuTotal - m.previousCPUTotal
		idleDelta := cpuIdle - m.previousCPUIdle
		if totalDelta > 0 {
			cpuPercent = clampPercent((totalDelta - idleDelta) / totalDelta * 100)
		}
	}
	m.previousCPUTotal = cpuTotal
	m.previousCPUIdle = cpuIdle
	m.hasPreviousCPU = true

	network := networkCounters[0]
	netRXPerSec := 0.0
	netTXPerSec := 0.0
	if m.hasPreviousNet {
		elapsed := sampledAt.Sub(m.previousNetAt).Seconds()
		if elapsed > 0 {
			if network.BytesRecv >= m.previousNetRX {
				netRXPerSec = float64(network.BytesRecv-m.previousNetRX) / elapsed
			}
			if network.BytesSent >= m.previousNetTX {
				netTXPerSec = float64(network.BytesSent-m.previousNetTX) / elapsed
			}
		}
	}
	m.previousNetRX = network.BytesRecv
	m.previousNetTX = network.BytesSent
	m.previousNetAt = sampledAt
	m.hasPreviousNet = true

	gpu := collectGPUUsage()
	return Sample{
		Timestamp:         sampledAt.Format(time.RFC3339Nano),
		CPUPercent:        roundValue(cpuPercent),
		GPUUsedPercent:    roundValue(gpu.Percent),
		GPUAvailable:      gpu.Available,
		GPUSource:         gpu.Source,
		GPUDeviceCount:    gpu.DeviceCount,
		MemoryUsedPercent: roundValue(clampPercent(memory.UsedPercent)),
		MemoryUsedBytes:   memory.Used,
		MemoryTotalBytes:  memory.Total,
		DiskUsedPercent:   roundValue(clampPercent(diskUsage.UsedPercent)),
		DiskUsedBytes:     diskUsage.Used,
		DiskTotalBytes:    diskUsage.Total,
		NetRXBytesPerSec:  roundValue(netRXPerSec),
		NetTXBytesPerSec:  roundValue(netTXPerSec),
		NetRXTotalBytes:   network.BytesRecv,
		NetTXTotalBytes:   network.BytesSent,
	}, nil
}

func (m *Monitor) append(sampledAt time.Time, sample Sample) error {
	line, err := json.Marshal(sample)
	if err != nil {
		return err
	}
	path := m.path(sampledAt.Format("2006-01-02"))
	m.fileMu.Lock()
	defer m.fileMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func (m *Monitor) Query(date, mode string) (UsageResponse, error) {
	mode = normalizeMode(mode)
	selected, err := parseDate(date)
	if err != nil {
		return UsageResponse{}, err
	}
	if mode == "day" {
		return m.readDay(selected)
	}
	start, end := viewPeriod(selected, mode)
	return m.readPeriod(start, end, mode)
}

func (m *Monitor) readPeriod(start, end time.Time, mode string) (UsageResponse, error) {
	response := newResponse(start, end, mode, "day")
	dates, err := m.listDates()
	if err != nil {
		return response, err
	}
	response.Dates = dates
	for current := start; !current.After(end); current = current.AddDate(0, 0, 1) {
		day, err := m.readDay(current)
		if err != nil {
			return response, err
		}
		response.Points = append(response.Points, aggregateDay(current, day.Points))
		response.RawSampleCount += day.RawSampleCount
		response.InvalidSampleCount += day.InvalidSampleCount
		response.Truncated = response.Truncated || day.Truncated
		if day.Latest != nil && (response.Latest == nil || day.Latest.Timestamp > response.Latest.Timestamp) {
			copy := *day.Latest
			response.Latest = &copy
		}
	}
	response.BucketCount = len(response.Points)
	response.BucketSeconds = 24 * 60 * 60
	return response, nil
}

func (m *Monitor) readDay(dayStart time.Time) (UsageResponse, error) {
	date := dayStart.Format("2006-01-02")
	response := newResponse(dayStart, dayStart, "day", "five_minutes")
	dates, err := m.listDates()
	if err != nil {
		return response, err
	}
	response.Dates = dates

	m.fileMu.RLock()
	defer m.fileMu.RUnlock()
	file, err := os.Open(m.path(date))
	if err != nil {
		if os.IsNotExist(err) {
			response.Points = buildPoints(dayStart, nil)
			return response, nil
		}
		return response, err
	}
	defer file.Close()

	limited := &io.LimitedReader{R: file, N: maxQueryBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	buckets := map[int]*accumulator{}
	var latestAt time.Time
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var sample Sample
		if err := json.Unmarshal([]byte(line), &sample); err != nil {
			response.InvalidSampleCount++
			continue
		}
		sampledAt, err := time.Parse(time.RFC3339Nano, sample.Timestamp)
		if err != nil || sampledAt.In(time.Local).Format("2006-01-02") != date {
			response.InvalidSampleCount++
			continue
		}
		sampledAt = sampledAt.In(time.Local)
		response.RawSampleCount++
		if response.Latest == nil || sampledAt.After(latestAt) {
			copy := sample
			response.Latest = &copy
			latestAt = sampledAt
		}
		index := (sampledAt.Hour()*60 + sampledAt.Minute()) / int(bucketDuration/time.Minute)
		bucket := buckets[index]
		if bucket == nil {
			bucket = &accumulator{start: dayStart.Add(time.Duration(index) * bucketDuration)}
			buckets[index] = bucket
		}
		bucket.addSample(sample)
	}
	if err := scanner.Err(); err != nil {
		return response, err
	}
	response.Truncated = limited.N <= 0
	response.Points = buildPoints(dayStart, buckets)
	return response, nil
}

func (m *Monitor) listDates() ([]string, error) {
	m.fileMu.RLock()
	defer m.fileMu.RUnlock()
	entries, err := os.ReadDir(m.dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	dates := make([]string, 0, len(entries))
	oldest := beginningOfDay(time.Now()).AddDate(0, 0, -(RetentionDays - 1))
	today := beginningOfDay(time.Now())
	for _, entry := range entries {
		date, ok := dateFromFileName(entry.Name())
		if !entry.IsDir() && ok && !date.Before(oldest) && !date.After(today) {
			dates = append(dates, date.Format("2006-01-02"))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	return dates, nil
}

func (m *Monitor) prune(now time.Time) (int, error) {
	m.fileMu.Lock()
	defer m.fileMu.Unlock()
	entries, err := os.ReadDir(m.dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	oldest := beginningOfDay(now).AddDate(0, 0, -(RetentionDays - 1))
	removed := 0
	for _, entry := range entries {
		date, ok := dateFromFileName(entry.Name())
		if entry.IsDir() || !ok || !date.Before(oldest) {
			continue
		}
		if err := os.Remove(filepath.Join(m.dataDir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func (m *Monitor) path(date string) string {
	return filepath.Join(m.dataDir, date+".jsonl")
}

func (m *Monitor) reportError(err error) {
	if err == nil {
		return
	}
	now := time.Now()
	if !m.lastErrorLogAt.IsZero() && now.Sub(m.lastErrorLogAt) < 5*time.Minute {
		return
	}
	m.lastErrorLogAt = now
	log.Printf("system resource monitor: %v", err)
}

func (a *accumulator) addSample(sample Sample) {
	a.count++
	a.cpu += sample.CPUPercent
	a.memory += sample.MemoryUsedPercent
	a.disk += sample.DiskUsedPercent
	a.netRX += sample.NetRXBytesPerSec
	a.netTX += sample.NetTXBytesPerSec
	if sample.GPUAvailable {
		a.gpuCount++
		a.gpu += sample.GPUUsedPercent
	}
}

func (a accumulator) point(timestamp time.Time, label string) Point {
	point := Point{Timestamp: timestamp.Format(time.RFC3339), TimeLabel: label, SampleCount: a.count}
	if a.count > 0 {
		divisor := float64(a.count)
		point.CPUPercent = roundValue(a.cpu / divisor)
		point.MemoryUsedPercent = roundValue(a.memory / divisor)
		point.DiskUsedPercent = roundValue(a.disk / divisor)
		point.NetRXBytesPerSec = roundValue(a.netRX / divisor)
		point.NetTXBytesPerSec = roundValue(a.netTX / divisor)
	}
	if a.gpuCount > 0 {
		point.GPUAvailable = true
		point.GPUSampleCount = a.gpuCount
		point.GPUUsedPercent = roundValue(a.gpu / float64(a.gpuCount))
	}
	return point
}

func buildPoints(dayStart time.Time, buckets map[int]*accumulator) []Point {
	points := make([]Point, 0, bucketCount)
	for index := 0; index < bucketCount; index++ {
		at := dayStart.Add(time.Duration(index) * bucketDuration)
		bucket := accumulator{}
		if buckets[index] != nil {
			bucket = *buckets[index]
		}
		points = append(points, bucket.point(at, at.Format("15:04")))
	}
	return points
}

func aggregateDay(day time.Time, points []Point) Point {
	result := accumulator{}
	for _, point := range points {
		if point.SampleCount > 0 {
			result.count += point.SampleCount
			result.cpu += point.CPUPercent * float64(point.SampleCount)
			result.memory += point.MemoryUsedPercent * float64(point.SampleCount)
			result.disk += point.DiskUsedPercent * float64(point.SampleCount)
			result.netRX += point.NetRXBytesPerSec * float64(point.SampleCount)
			result.netTX += point.NetTXBytesPerSec * float64(point.SampleCount)
		}
		if point.GPUAvailable && point.GPUSampleCount > 0 {
			result.gpuCount += point.GPUSampleCount
			result.gpu += point.GPUUsedPercent * float64(point.GPUSampleCount)
		}
	}
	return result.point(day, day.Format("01/02"))
}

func newResponse(start, end time.Time, mode, unit string) UsageResponse {
	return UsageResponse{
		Success:               true,
		Date:                  start.Format("2006-01-02"),
		RangeStart:            start.Format("2006-01-02"),
		RangeEnd:              end.Format("2006-01-02"),
		ViewMode:              mode,
		Unit:                  unit,
		Dates:                 []string{},
		Points:                []Point{},
		SampleIntervalSeconds: int(SampleInterval / time.Second),
		BucketSeconds:         int(bucketDuration / time.Second),
		BucketCount:           bucketCount,
		RetentionDays:         RetentionDays,
	}
}

func parseDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return beginningOfDay(time.Now()), nil
	}
	date, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil || date.Format("2006-01-02") != value {
		return time.Time{}, fmt.Errorf("日期格式必須為 YYYY-MM-DD")
	}
	return date, nil
}

func viewPeriod(selected time.Time, mode string) (time.Time, time.Time) {
	if mode == "week" {
		daysSinceMonday := (int(selected.Weekday()) + 6) % 7
		start := selected.AddDate(0, 0, -daysSinceMonday)
		return start, start.AddDate(0, 0, 6)
	}
	start := time.Date(selected.Year(), selected.Month(), 1, 0, 0, 0, 0, selected.Location())
	return start, start.AddDate(0, 1, -1)
}

func normalizeMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "week":
		return "week"
	case "month":
		return "month"
	default:
		return "day"
	}
}

func beginningOfDay(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func dateFromFileName(name string) (time.Time, bool) {
	if filepath.Ext(name) != ".jsonl" {
		return time.Time{}, false
	}
	date := strings.TrimSuffix(name, ".jsonl")
	parsed, err := time.ParseInLocation("2006-01-02", date, time.Local)
	return parsed, err == nil && parsed.Format("2006-01-02") == date
}

func cpuTotals(value cpu.TimesStat) (float64, float64) {
	total := value.User + value.System + value.Idle + value.Nice + value.Iowait + value.Irq + value.Softirq + value.Steal
	return total, value.Idle + value.Iowait
}

func clampPercent(value float64) float64 {
	return math.Max(0, math.Min(100, value))
}

func roundValue(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func firstError(err error, fallback string) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("%s", fallback)
}
