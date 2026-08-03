package cronparser

import (
	"testing"
	"time"

	_ "time/tzdata"
)

// TestLastDayOfMonthMatch isolates why "L" with a restricted month stalls.
func TestLastDayOfMonthMatch(t *testing.T) {
	e, err := Parse("4 14 L 2 *", Options{TZ: "UTC", CurrentDate: "2027-01-01T00:00:00"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	t.Logf("dom: values=%v chars=%v hasLast=%v wildcard=%v raw=%q",
		e.fields.DayOfMonth.values, e.fields.DayOfMonth.chars,
		e.fields.DayOfMonth.hasLastChar, e.fields.DayOfMonth.wildcard,
		e.fields.DayOfMonth.rawValue)
	t.Logf("dow: values=%v hasLast=%v wildcard=%v raw=%q",
		e.fields.DayOfWeek.values, e.fields.DayOfWeek.hasLastChar,
		e.fields.DayOfWeek.wildcard, e.fields.DayOfWeek.rawValue)
	t.Logf("month: values=%v", e.fields.Month.values)

	// Feb 2027 has 28 days, so Feb 28 is the last day.
	for _, day := range []int{27, 28} {
		d := newCronDate(time.Date(2027, 2, day, 14, 4, 0, 0, time.UTC), time.UTC)
		t.Logf("Feb %d 2027: IsLastDayOfMonth=%v matchDayOfMonth=%v",
			day, d.IsLastDayOfMonth(), e.matchDayOfMonth(d))
	}

	// And the whole point: does iteration find it?
	e2, _ := Parse("4 14 L 2 *", Options{TZ: "UTC", CurrentDate: "2027-01-01T00:00:00"})
	got, err := e2.Next()
	if err != nil {
		t.Errorf("Next() error = %v; want 2027-02-28 14:04", err)
	} else {
		t.Logf("Next() = %s", got.Format("2006-01-02 15:04:05"))
	}
}

// TestDayOfWeekWildcardWithL checks the interaction that governs which
// matchDayOfMonth rule applies when dom is "L" and dow is "*".
func TestDayOfWeekWildcardWithL(t *testing.T) {
	e, err := Parse("0 0 L * *", Options{TZ: "UTC", CurrentDate: "2026-01-01T00:00:00"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// dom="L" is NOT a wildcard; dow="*" IS. So rule 2 must fire:
	//   matchedDOM && !isRestrictedDow
	if e.fields.DayOfMonth.IsWildcard() {
		t.Error("dom L should not be wildcard")
	}
	if !e.fields.DayOfWeek.IsWildcard() {
		t.Error("dow * should be wildcard")
	}
	d := newCronDate(time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), time.UTC)
	if !e.matchDayOfMonth(d) {
		t.Error("Jan 31 should match L")
	}
}
