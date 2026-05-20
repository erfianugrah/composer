package handler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSplitDockerTimestamp(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantOK    bool
		wantMsg   string
		wantTSStr string // RFC3339Nano form for compare
	}{
		{
			name:      "rfc3339nano utc",
			in:        "2026-05-20T08:35:09.850421Z 2026-05-20 08:35:09.850 UTC [113] LOG: checkpoint starting: time",
			wantOK:    true,
			wantMsg:   "2026-05-20 08:35:09.850 UTC [113] LOG: checkpoint starting: time",
			wantTSStr: "2026-05-20T08:35:09.850421Z",
		},
		{
			name:      "rfc3339 second precision",
			in:        "2026-05-20T08:35:09Z hello",
			wantOK:    true,
			wantMsg:   "hello",
			wantTSStr: "2026-05-20T08:35:09Z",
		},
		{
			name:      "rfc3339 with offset",
			in:        "2026-05-20T10:35:09.123+02:00 oslo morning",
			wantOK:    true,
			wantMsg:   "oslo morning",
			wantTSStr: "2026-05-20T10:35:09.123+02:00",
		},
		{
			name:   "no space — single token",
			in:     "noseparatorhere",
			wantOK: false,
		},
		{
			name:   "leading space — empty prefix",
			in:    " message starts with space",
			wantOK: false,
		},
		{
			name:   "non-timestamp prefix",
			in:    "INFO startup complete",
			wantOK: false,
		},
		{
			name:   "empty message after timestamp",
			in:     "2026-05-20T08:35:09Z ",
			wantOK: true,
			wantMsg: "",
			wantTSStr: "2026-05-20T08:35:09Z",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts, msg, ok := splitDockerTimestamp(tc.in)
			assert.Equal(t, tc.wantOK, ok)
			if !tc.wantOK {
				// Fallback contract: message preserved as-is so caller can still send it.
				assert.Equal(t, tc.in, msg)
				assert.True(t, ts.IsZero())
				return
			}
			assert.Equal(t, tc.wantMsg, msg)
			want, perr := time.Parse(time.RFC3339Nano, tc.wantTSStr)
			assert.NoError(t, perr)
			assert.True(t, ts.Equal(want), "ts=%s want=%s", ts.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
		})
	}
}

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
