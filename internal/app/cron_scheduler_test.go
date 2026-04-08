package app

import (
	"strings"
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
		// Exact matches
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
		{"midnight doesn't match 15:30", "0 0 * * *", false},

		// Step expressions (*/N)
		{"*/5 matches 30", "*/5 * * * *", true},   // 30 % 5 == 0
		{"*/7 no match 30", "*/7 * * * *", false}, // 30 % 7 != 0
		{"*/15 matches 30", "*/15 * * * *", true},
		{"hour */3 matches 15", "* */3 * * *", true},   // 15 % 3 == 0
		{"hour */4 no match 15", "* */4 * * *", false}, // 15 % 4 != 0

		// Range expressions (N-M)
		{"range 25-35 matches 30", "25-35 * * * *", true},
		{"range 31-45 no match 30", "31-45 * * * *", false},
		{"hour range 14-16 matches 15", "* 14-16 * * *", true},
		{"weekday range 1-5 matches 3", "* * * * 1-5", true},

		// List expressions (N,M,O)
		{"list 0,15,30,45 matches 30", "0,15,30,45 * * * *", true},
		{"list 0,15,45 no match 30", "0,15,45 * * * *", false},
		{"month list 3,4,5 matches 4", "* * * 3,4,5 *", true},

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
		fields := strings.Fields(tt.input)
		assert.Len(t, fields, tt.want, "strings.Fields(%q)", tt.input)
	}
}
