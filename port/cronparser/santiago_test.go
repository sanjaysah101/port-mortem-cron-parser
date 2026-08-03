package cronparser

import (
	"testing"
	"time"

	_ "time/tzdata"
)

// TestSantiagoMidnightGap covers the case where the DST transition happens AT
// MIDNIGHT, so the local day has no 00:00. Every zone tested before this had a
// 01:00-03:00 transition, which left this path unexercised.
//
// America/Santiago 2026-09-06: the local day begins at 01:00 -03:00.
func TestSantiagoMidnightGap(t *testing.T) {
	loc, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Fatal(err)
	}

	// Midnight does not exist on this date.
	naive := time.Date(2026, 9, 6, 0, 0, 0, 0, loc)
	t.Logf("time.Date(2026-09-06 00:00 Santiago) = %s", naive.Format(time.RFC3339))
	if naive.Hour() == 0 {
		t.Logf("  Go kept hour 0 (offset %s)", naive.Format("-07:00"))
	} else {
		t.Logf("  Go shifted to hour %d", naive.Hour())
	}

	// SetStartOfDay must not produce a time that breaks day comparison.
	d := newCronDate(time.Date(2026, 9, 6, 12, 0, 0, 0, loc), loc)
	start := d.clone()
	start.SetStartOfDay()
	end := d.clone()
	end.SetEndOfDay()
	t.Logf("startOfDay = %s (offset %d)", start.String(), start.UTCOffsetMinutes())
	t.Logf("endOfDay   = %s (offset %d)", end.String(), end.UTCOffsetMinutes())

	if start.Day() != 6 {
		t.Errorf("startOfDay moved off the requested day: got day %d, want 6", start.Day())
	}

	// Both offsets are -180 here, so checkDstTransition() reports FALSE for
	// Santiago's transition day. That is NOT a port bug — luxon does the same:
	//
	//   DateTime.fromISO('2026-09-06T12:00', {zone:'America/Santiago'})
	//     .startOf('day')  -> 2026-09-06 01:00 -03:00  (offset -180)
	//     .endOf('day')    -> 2026-09-06 23:59 -03:00  (offset -180)
	//
	// Because startOf('day') clamps PAST the gap, the pre-transition offset is
	// never sampled. So cron-parser's own DST-transition detector is blind to
	// zones whose transition happens at midnight, and the port reproduces that
	// blindness faithfully. Asserting a difference here would be asserting a
	// divergence from upstream.
	if start.UTCOffsetMinutes() != end.UTCOffsetMinutes() {
		t.Errorf("expected startOf/endOf to share an offset (matching luxon), got %d and %d",
			start.UTCOffsetMinutes(), end.UTCOffsetMinutes())
	}
}

// TestSantiagoIterationTerminates is the regression guard: these expressions
// caused "loop limit exceeded" while TypeScript returned results.
func TestSantiagoIterationTerminates(t *testing.T) {
	cases := []struct {
		expr string
		from string
	}{
		{"* * L 11/7 *", "2026-06-15T08:45:00"},
		{"12 */14 L 10 ?", "2026-10-04T16:29:00"},
		{"* * 22-23 19/5 12-12/5 */14", "2026-04-05T14:15:00"},
		{"58-59/4 0 ? 9/9 6-6/5", "2026-12-31T12:29:00"},
		{"7 17 26-27/5 6/8 ?", "2026-02-28T03:59:00"},
	}

	for _, c := range cases {
		e, err := Parse(c.expr, Options{TZ: "America/Santiago", CurrentDate: c.from})
		if err != nil {
			t.Errorf("Parse(%q): %v", c.expr, err)
			continue
		}
		got, err := e.Next()
		if err != nil {
			t.Errorf("Next() for %q from %s: %v", c.expr, c.from, err)
			continue
		}
		t.Logf("%-30q -> %s", c.expr, got.Format("2006-01-02 15:04:05 -07:00"))
	}
}
