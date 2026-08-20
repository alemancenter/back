package services

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type hostPerformanceMetrics struct {
	CPUUsage      float64
	CPUAvailable  bool
	CPUCores      int
	Load1         float64
	LoadAvailable bool

	MemoryTotal      int64
	MemoryUsed       int64
	MemoryFree       int64
	MemoryPercentage float64
	MemoryAvailable  bool

	DiskTotal      int64
	DiskUsed       int64
	DiskFree       int64
	DiskPercentage float64
	DiskAvailable  bool
}

type hostCPUSampler struct {
	mu    sync.Mutex
	idle  uint64
	total uint64
}

var performanceCPUSampler = newHostCPUSampler()

func newHostCPUSampler() *hostCPUSampler {
	idle, total, ok := readCPUCounters()
	if !ok {
		return &hostCPUSampler{}
	}

	return &hostCPUSampler{
		idle:  idle,
		total: total,
	}
}

func (s *hostCPUSampler) Usage() (float64, bool) {
	idle, total, ok := readCPUCounters()
	if !ok {
		return 0, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.total == 0 {
		s.idle = idle
		s.total = total
		return 0, false
	}

	usage, valid := cpuUsageFromDelta(
		s.idle,
		s.total,
		idle,
		total,
	)

	s.idle = idle
	s.total = total

	return usage, valid
}

func cpuUsageFromDelta(
	previousIdle uint64,
	previousTotal uint64,
	currentIdle uint64,
	currentTotal uint64,
) (float64, bool) {
	if currentTotal <= previousTotal ||
		currentIdle < previousIdle {
		return 0, false
	}

	totalDelta := currentTotal - previousTotal
	idleDelta := currentIdle - previousIdle

	if totalDelta == 0 || idleDelta > totalDelta {
		return 0, false
	}

	busyDelta := totalDelta - idleDelta

	return float64(busyDelta) * 100 / float64(totalDelta), true
}

func readCPUCounters() (idle uint64, total uint64, ok bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}

	return parseCPUCounters(string(data))
}

func parseCPUCounters(data string) (idle uint64, total uint64, ok bool) {
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)

		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}

		values := make([]uint64, 0, len(fields)-1)

		for _, field := range fields[1:] {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return 0, 0, false
			}

			values = append(values, value)
			total += value
		}

		if len(values) < 4 {
			return 0, 0, false
		}

		idle = values[3]

		// Linux /proc/stat: iowait follows idle.
		if len(values) > 4 {
			idle += values[4]
		}

		return idle, total, total > 0
	}

	return 0, 0, false
}

func readLoad1() (float64, bool) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, false
	}

	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, false
	}

	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, false
	}

	return value, true
}

func readHostMemory() (
	total int64,
	used int64,
	free int64,
	percentage float64,
	ok bool,
) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, 0, 0, false
	}

	totalBytes, availableBytes, valid :=
		parseMemInfo(string(data))

	if !valid {
		return 0, 0, 0, 0, false
	}

	total = int64(totalBytes)
	free = int64(availableBytes)

	if free > total {
		free = total
	}

	used = total - free

	if total > 0 {
		percentage =
			float64(used) * 100 / float64(total)
	}

	return total, used, free, percentage, true
}

func parseMemInfo(data string) (
	totalBytes uint64,
	availableBytes uint64,
	ok bool,
) {
	var totalKB uint64
	var availableKB uint64

	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			totalKB = value

		case "MemAvailable":
			availableKB = value
		}
	}

	if totalKB == 0 || availableKB == 0 {
		return 0, 0, false
	}

	return totalKB * 1024, availableKB * 1024, true
}

func readHostDisk() (
	total int64,
	used int64,
	free int64,
	percentage float64,
	ok bool,
) {
	var stat syscall.Statfs_t

	if err := syscall.Statfs("/", &stat); err != nil {
		return 0, 0, 0, 0, false
	}

	blockSize := uint64(stat.Bsize)

	totalBytes := stat.Blocks * blockSize
	freeBytes := stat.Bavail * blockSize

	if freeBytes > totalBytes {
		freeBytes = totalBytes
	}

	usedBytes := totalBytes - freeBytes

	total = int64(totalBytes)
	free = int64(freeBytes)
	used = int64(usedBytes)

	if totalBytes > 0 {
		percentage =
			float64(usedBytes) * 100 /
				float64(totalBytes)
	}

	return total, used, free, percentage, true
}

func readHostPerformance() hostPerformanceMetrics {
	cpuUsage, cpuAvailable :=
		performanceCPUSampler.Usage()

	load1, loadAvailable := readLoad1()

	memTotal,
		memUsed,
		memFree,
		memPercentage,
		memAvailable := readHostMemory()

	diskTotal,
		diskUsed,
		diskFree,
		diskPercentage,
		diskAvailable := readHostDisk()

	return hostPerformanceMetrics{
		CPUUsage:      cpuUsage,
		CPUAvailable:  cpuAvailable,
		CPUCores:      runtime.NumCPU(),
		Load1:         load1,
		LoadAvailable: loadAvailable,

		MemoryTotal:      memTotal,
		MemoryUsed:       memUsed,
		MemoryFree:       memFree,
		MemoryPercentage: memPercentage,
		MemoryAvailable:  memAvailable,

		DiskTotal:      diskTotal,
		DiskUsed:       diskUsed,
		DiskFree:       diskFree,
		DiskPercentage: diskPercentage,
		DiskAvailable:  diskAvailable,
	}
}
