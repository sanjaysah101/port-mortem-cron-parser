package cronparser

import (
	"testing"
	"time"

	_ "time/tzdata"
)

// TestGoAmbiguityResolution documents Go's time.Date behavior on a repeated
// wall-clock time, which is the root cause of the fall-back divergences.
//
// On 2026-11-01 in America/New_York, 01:30 occurs twice: once at -04:00 (EDT)
// and again at -05:00 (EST). Reconstructing the date from wall-clock components
// must pick one, and Go always picks the FIRST.
func TestGoAmbiguityResolution(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}

	// The transition is at 06:00 UTC (02:00 EDT -> 01:00 EST), so the SECOND
	// occurrence of 01:30 local is 06:30 UTC.
	second := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC).In(loc) // 01:30 -05:00
	_, off := second.Zone()
	if off/60 != -300 {
		t.Fatalf("setup: expected -300 offset, got %d", off/60)
	}

	// Now rebuild from that instant's own wall-clock fields, which is what
	// SetMinutes/SetHours do.
	rebuilt := time.Date(second.Year(), second.Month(), second.Day(),
		second.Hour(), second.Minute(), second.Second(), 0, loc)
	_, roff := rebuilt.Zone()

	t.Logf("original : %s (offset %d)", second.Format(time.RFC3339), off/60)
	t.Logf("rebuilt  : %s (offset %d)", rebuilt.Format(time.RFC3339), roff/60)

	if roff/60 == off/60 {
		t.Log("Go PRESERVED the offset — no snap-back")
	} else {
		t.Logf("Go SNAPPED BACK to offset %d: reconstructing from wall-clock "+
			"fields loses which occurrence we were in", roff/60)
	}

	// This is the trap: the rebuilt instant is EARLIER than the original,
	// so an iteration loop that mutates via wall-clock setters cannot advance
	// past the repeated hour.
	if rebuilt.Before(second) {
		t.Logf("rebuilt is %v EARLIER than original — iteration would not progress",
			second.Sub(rebuilt))
	}
}
