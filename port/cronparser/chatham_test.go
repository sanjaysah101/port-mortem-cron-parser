package cronparser

import (
	"testing"

	_ "time/tzdata"
)

// TestChathamPrevUpstreamBug documents a DIVERGENCE WHERE THE PORT IS CORRECT
// AND UPSTREAM IS NOT.
//
// Found by the differential fuzzer (round 63, seed 1909638832), then minimised.
//
// Upstream cron-parser 5.7.0 @ 8410d37:
//
//	CronExpressionParser
//	  .parse('0 0 0 * * *', {tz:'Pacific/Chatham', currentDate:'2026-09-27T05:45:00'})
//	  .prev()
//	=> Error: Invalid expression, loop limit exceeded
//
// A plain daily-midnight schedule. The trigger is a scheduled hour BELOW the
// spring-forward gap on Chatham's transition day, where local hours run:
//
//	00:00 +12:45 | 01:00 +12:45 | 02:00 +12:45 | 04:00 +13:45 | 05:00 +13:45
//	                                            ^^^^^ 03:00 does not exist
//
// Verified scope: hour 0 or 1 throws; hour 22 or "11,22" does not. Only on
// 2026-09-27 — the same expression works on Chatham's other days, on its
// fall-back day, and in UTC / America/New_York. So it needs a :45-offset zone
// AND a spring-forward day AND a target hour before the gap.
//
// The port returns the correct answers. Iterating backward from 05:45 on a
// */5-minute schedule, the previous fires are 00:55, 00:50, 00:45 local — which
// upstream ITSELF returns when started from 01:00 instead of 05:45, confirming
// those values are right and the failure is in reverse hour-stepping across the
// gap, not in the schedule.
//
// This divergence is INTENTIONALLY NOT reproduced. The port could be made to
// throw here, but propagating a hang into a rewrite is not fidelity worth having,
// and the correct values are not in question. Recorded in DECISIONS.md D11 and
// reportable upstream.
func TestChathamPrevUpstreamBug(t *testing.T) {
	cases := []struct {
		name string
		expr string
	}{
		{"minimal daily midnight", "0 0 0 * * *"},
		{"hour 1", "0 0 1,11 * * *"},
		{"hour set including 0", "0 0 0,11,22 * * *"},
		{"as found by fuzzer", "*/5 */11 29-29 */2 0/8"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, err := Parse(c.expr, Options{
				TZ:          "Pacific/Chatham",
				CurrentDate: "2026-09-27T05:45:00",
			})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got, err := e.Prev()
			if err != nil {
				t.Fatalf("Prev() = %v; the port must NOT reproduce upstream's "+
					"loop-limit failure here", err)
			}
			t.Logf("Prev() = %s  (upstream throws)", got.Format("2006-01-02 15:04:05 -07:00"))
		})
	}
}

// TestChathamPrevControlCases pins the cases where upstream SUCCEEDS, so the
// port is checked against real expected values rather than only against the
// bug's absence.
func TestChathamPrevControlCases(t *testing.T) {
	cases := []struct {
		expr string
		from string
		want string // upstream's answer, in UTC
	}{
		// Same expression, started before the gap: upstream returns this.
		{"*/5 */11 29-29 */2 0/8", "2026-09-27T01:00:00", "2026-09-26T12:10:00Z"},
		// Hour above the gap: upstream succeeds.
		{"0 0 22 * * *", "2026-09-27T05:45:00", "2026-09-26T09:15:00Z"},
		{"0 0 11,22 * * *", "2026-09-27T05:45:00", "2026-09-26T09:15:00Z"},
		// Non-transition day.
		{"*/5 */11 29-29 */2 0/8", "2026-09-20T05:45:00", "2026-09-19T12:10:00Z"},
	}

	for _, c := range cases {
		e, err := Parse(c.expr, Options{TZ: "Pacific/Chatham", CurrentDate: c.from})
		if err != nil {
			t.Errorf("parse(%q): %v", c.expr, err)
			continue
		}
		got, err := e.Prev()
		if err != nil {
			t.Errorf("Prev() for %q from %s: %v", c.expr, c.from, err)
			continue
		}
		if g := got.UTC().Format("2006-01-02T15:04:05Z"); g != c.want {
			t.Errorf("Prev() for %q from %s = %s, want %s (upstream's value)",
				c.expr, c.from, g, c.want)
		}
	}
}
