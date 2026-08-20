package services

import (
	"math"
	"testing"
)

func TestParseCPUCounters(t *testing.T) {
	data := "cpu  100 20 30 400 50 10 5 0 0 0\n"

	idle, total, ok := parseCPUCounters(data)

	if !ok {
		t.Fatal("parseCPUCounters returned not ok")
	}

	if idle != 450 {
		t.Fatalf("idle = %d, want 450", idle)
	}

	if total != 615 {
		t.Fatalf("total = %d, want 615", total)
	}
}

func TestCPUUsageFromDelta(t *testing.T) {
	usage, ok := cpuUsageFromDelta(
		800,
		1000,
		850,
		1200,
	)

	if !ok {
		t.Fatal("cpuUsageFromDelta returned not ok")
	}

	if math.Abs(usage-75.0) > 0.001 {
		t.Fatalf(
			"usage = %.4f, want 75.0",
			usage,
		)
	}
}

func TestParseMemInfo(t *testing.T) {
	data := `
MemTotal:       1000 kB
MemFree:         100 kB
MemAvailable:    250 kB
`

	total, available, ok := parseMemInfo(data)

	if !ok {
		t.Fatal("parseMemInfo returned not ok")
	}

	if total != 1000*1024 {
		t.Fatalf(
			"total = %d, want %d",
			total,
			1000*1024,
		)
	}

	if available != 250*1024 {
		t.Fatalf(
			"available = %d, want %d",
			available,
			250*1024,
		)
	}
}

func TestLinuxHostSourcesReadable(t *testing.T) {
	_, totalCPU, cpuOK := readCPUCounters()

	if !cpuOK || totalCPU == 0 {
		t.Fatal("/proc/stat CPU counters unavailable")
	}

	totalMem, _, _, _, memOK := readHostMemory()

	if !memOK || totalMem <= 0 {
		t.Fatal("/proc/meminfo unavailable")
	}

	totalDisk, _, _, _, diskOK := readHostDisk()

	if !diskOK || totalDisk <= 0 {
		t.Fatal("host statfs unavailable")
	}

	if _, loadOK := readLoad1(); !loadOK {
		t.Fatal("/proc/loadavg unavailable")
	}
}
