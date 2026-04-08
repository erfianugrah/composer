package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseDockerStats_CPUCalculation(t *testing.T) {
	raw := &dockerStats{}
	raw.CPUStats.CPUUsage.TotalUsage = 200_000_000
	raw.CPUStats.SystemCPUUsage = 1_000_000_000
	raw.CPUStats.OnlineCPUs = 4
	raw.PreCPUStats.CPUUsage.TotalUsage = 100_000_000
	raw.PreCPUStats.SystemCPUUsage = 500_000_000

	stats := parseDockerStats("abc123", raw)

	// CPU delta = 100M, system delta = 500M, 4 CPUs
	// (100M / 500M) * 4 * 100 = 80%
	assert.InDelta(t, 80.0, stats.CPUPercent, 0.1)
	assert.Equal(t, "abc123", stats.ContainerID)
}

func TestParseDockerStats_Memory(t *testing.T) {
	raw := &dockerStats{}
	raw.MemoryStats.Usage = 256 * 1024 * 1024  // 256 MB
	raw.MemoryStats.Limit = 1024 * 1024 * 1024 // 1 GB

	stats := parseDockerStats("test", raw)

	assert.Equal(t, uint64(256*1024*1024), stats.MemUsage)
	assert.Equal(t, uint64(1024*1024*1024), stats.MemLimit)
	assert.InDelta(t, 25.0, stats.MemPercent, 0.1)
}

func TestParseDockerStats_Network(t *testing.T) {
	raw := &dockerStats{}
	raw.Networks = map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	}{
		"eth0": {RxBytes: 1000, TxBytes: 2000},
		"eth1": {RxBytes: 500, TxBytes: 300},
	}

	stats := parseDockerStats("test", raw)

	assert.Equal(t, uint64(1500), stats.NetRx)
	assert.Equal(t, uint64(2300), stats.NetTx)
}

func TestParseDockerStats_BlockIO(t *testing.T) {
	raw := &dockerStats{}
	raw.BlkioStats.IOServiceBytesRecursive = []struct {
		Op    string `json:"op"`
		Value uint64 `json:"value"`
	}{
		{Op: "read", Value: 4096},
		{Op: "write", Value: 8192},
		{Op: "Read", Value: 1024}, // capitalize variant
	}

	stats := parseDockerStats("test", raw)

	assert.Equal(t, uint64(5120), stats.BlockRead)
	assert.Equal(t, uint64(8192), stats.BlockWrite)
}

func TestParseDockerStats_PIDs(t *testing.T) {
	raw := &dockerStats{}
	raw.PidsStats.Current = 42

	stats := parseDockerStats("test", raw)
	assert.Equal(t, uint64(42), stats.PIDs)
}

func TestParseDockerStats_ZeroDivision(t *testing.T) {
	// No delta -- should not panic or produce NaN
	raw := &dockerStats{}
	stats := parseDockerStats("test", raw)

	assert.Equal(t, 0.0, stats.CPUPercent)
	assert.Equal(t, 0.0, stats.MemPercent)
}
