package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// matchCronField edge cases
// ---------------------------------------------------------------------------

func TestMatchCronField_Wildcard(t *testing.T) {
	assert.True(t, matchCronField("*", 5, 59))
	assert.True(t, matchCronField("*", 0, 59))
	assert.True(t, matchCronField("*", 59, 59))
}

func TestMatchCronField_Exact(t *testing.T) {
	assert.True(t, matchCronField("5", 5, 59))
	assert.False(t, matchCronField("5", 6, 59))
	assert.True(t, matchCronField("0", 0, 59))
}

func TestMatchCronField_Range(t *testing.T) {
	assert.True(t, matchCronField("1-5", 1, 59))
	assert.True(t, matchCronField("1-5", 3, 59))
	assert.True(t, matchCronField("1-5", 5, 59))
	assert.False(t, matchCronField("1-5", 0, 59))
	assert.False(t, matchCronField("1-5", 6, 59))
}

func TestMatchCronField_Step(t *testing.T) {
	assert.True(t, matchCronField("*/15", 0, 59))
	assert.True(t, matchCronField("*/15", 15, 59))
	assert.True(t, matchCronField("*/15", 30, 59))
	assert.True(t, matchCronField("*/15", 45, 59))
	assert.False(t, matchCronField("*/15", 7, 59))
	assert.False(t, matchCronField("*/15", 14, 59))
}

func TestMatchCronField_StepEveryOne(t *testing.T) {
	// */1 should match everything (like *)
	assert.True(t, matchCronField("*/1", 0, 59))
	assert.True(t, matchCronField("*/1", 37, 59))
}

func TestMatchCronField_List(t *testing.T) {
	assert.True(t, matchCronField("1,3,5", 1, 59))
	assert.True(t, matchCronField("1,3,5", 3, 59))
	assert.True(t, matchCronField("1,3,5", 5, 59))
	assert.False(t, matchCronField("1,3,5", 2, 59))
	assert.False(t, matchCronField("1,3,5", 4, 59))
}

func TestMatchCronField_RangeWithStep(t *testing.T) {
	// 1-10/3 should match 1, 4, 7, 10
	assert.True(t, matchCronField("1-10/3", 1, 59))
	assert.True(t, matchCronField("1-10/3", 4, 59))
	assert.True(t, matchCronField("1-10/3", 7, 59))
	assert.True(t, matchCronField("1-10/3", 10, 59))
	assert.False(t, matchCronField("1-10/3", 2, 59))
	assert.False(t, matchCronField("1-10/3", 0, 59))
	assert.False(t, matchCronField("1-10/3", 11, 59))
}

func TestMatchCronField_InvalidInput(t *testing.T) {
	assert.False(t, matchCronField("abc", 5, 59))
	assert.False(t, matchCronField("*/0", 0, 59))  // step 0 invalid
	assert.False(t, matchCronField("*/-1", 0, 59)) // negative step
	assert.False(t, matchCronField("a-b", 5, 59))  // non-numeric range
}

func TestMatchCronField_ListWithRanges(t *testing.T) {
	// "1-3,7,10-12" — combination of ranges and exact values in a list
	assert.True(t, matchCronField("1-3,7,10-12", 2, 59))
	assert.True(t, matchCronField("1-3,7,10-12", 7, 59))
	assert.True(t, matchCronField("1-3,7,10-12", 11, 59))
	assert.False(t, matchCronField("1-3,7,10-12", 5, 59))
	assert.False(t, matchCronField("1-3,7,10-12", 9, 59))
}

// ---------------------------------------------------------------------------
// shouldRunCron edge cases
// ---------------------------------------------------------------------------

func TestShouldRunCron_EveryMinute(t *testing.T) {
	now := time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC)
	assert.True(t, shouldRunCron("* * * * *", now))
}

func TestShouldRunCron_SpecificTime(t *testing.T) {
	now := time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC) // Sunday
	assert.True(t, shouldRunCron("30 14 * * *", now))
	assert.False(t, shouldRunCron("31 14 * * *", now))
}

func TestShouldRunCron_Midnight(t *testing.T) {
	midnight := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.True(t, shouldRunCron("0 0 * * *", midnight))
	assert.True(t, shouldRunCron("0 0 1 1 *", midnight))
	assert.False(t, shouldRunCron("0 0 2 1 *", midnight))
}

func TestShouldRunCron_EndOfDay(t *testing.T) {
	eod := time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC)
	assert.True(t, shouldRunCron("59 23 * * *", eod))
	assert.True(t, shouldRunCron("59 23 31 12 *", eod))
}

func TestShouldRunCron_WeekdayCheck(t *testing.T) {
	// 2025-06-15 is a Sunday (weekday 0)
	sunday := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	assert.True(t, shouldRunCron("0 12 * * 0", sunday))
	assert.False(t, shouldRunCron("0 12 * * 1", sunday)) // Monday
}

func TestShouldRunCron_InvalidExpr(t *testing.T) {
	now := time.Now()
	assert.False(t, shouldRunCron("invalid", now))
	assert.False(t, shouldRunCron("", now))
	assert.False(t, shouldRunCron("* * *", now))       // too few fields
	assert.False(t, shouldRunCron("* * * * * *", now)) // too many fields
}

func TestShouldRunCron_ComplexExpr(t *testing.T) {
	// 2026-04-08 15:30 Wednesday (weekday 3)
	now := time.Date(2026, 4, 8, 15, 30, 0, 0, time.UTC)

	// Every 15 min, hours 9-17, weekdays only
	assert.True(t, shouldRunCron("*/15 9-17 * * 1-5", now))

	// Every 15 min, hours 9-17, weekends only
	assert.False(t, shouldRunCron("*/15 9-17 * * 6,0", now))

	// 30th minute, 15th hour, 8th day, April, Wednesday — exact match
	assert.True(t, shouldRunCron("30 15 8 4 3", now))
}
