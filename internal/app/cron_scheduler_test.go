package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestShouldRunCron(t *testing.T) {
	// Fixed time: 2026-04-08 15:30:00 (Wednesday = weekday 3)
	now := time.Date(2026, 4, 8, 15, 30, 0, 0, time.UTC)

	tests := []struct {
		name string
		expr string
		want bool
	}{
		// Matches
		{"exact match", "30 15 8 4 3", true},
		{"all wildcards", "* * * * *", true},
		{"minute only", "30 * * * *", true},
		{"hour only", "* 15 * * *", true},
		{"every day at 15:30", "30 15 * * *", true},
		{"8th of month", "* * 8 * *", true},
		{"April", "* * * 4 *", true},
		{"Wednesday", "* * * * 3", true},

		// No match
		{"wrong minute", "31 15 8 4 3", false},
		{"wrong hour", "30 14 8 4 3", false},
		{"wrong day", "30 15 9 4 3", false},
		{"wrong month", "30 15 8 5 3", false},
		{"wrong weekday", "30 15 8 4 4", false},

		// Midnight -- doesn't match 15:30
		{"midnight doesn't match 15:30", "0 0 * * *", false},

		// Invalid
		{"too few fields", "30 15 8", false},
		{"too many fields", "30 15 8 4 3 extra", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRunCron(tt.expr, now)
			assert.Equal(t, tt.want, got, "shouldRunCron(%q, %v)", tt.expr, now)
		})
	}
}

func TestSplitFields(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"30 15 8 4 3", 5},
		{"* * * * *", 5},
		{"0 3 * * *", 5},
		{"  30  15  *  *  * ", 5},
		{"", 0},
		{"single", 1},
	}

	for _, tt := range tests {
		fields := splitFields(tt.input)
		assert.Len(t, fields, tt.want, "splitFields(%q)", tt.input)
	}
}
