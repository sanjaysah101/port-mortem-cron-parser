package cronparser

import (
	"strings"
	"testing"
)

// TestGetRawFieldsPadding locks down upstream's left-padding of omitted fields.
//
// Upstream: atoms.unshift(...defaults.slice(atoms.length))
//
// The seconds default is "0", NOT "*". Getting this wrong makes every 5-field
// expression fire once per second, which is how it was caught here — the
// differential harness reported 0/15 with the port stepping 00:30:01, :02, :03.
func TestGetRawFieldsPadding(t *testing.T) {
	cases := []struct {
		in   string
		want []string // second, minute, hour, dom, month, dow
	}{
		{"F0", []string{"*", "*", "*", "*", "0", "F0"}},
		{"F0 F1", []string{"*", "*", "*", "0", "F0", "F1"}},
		{"F0 F1 F2", []string{"*", "*", "0", "F0", "F1", "F2"}},
		{"F0 F1 F2 F3", []string{"*", "0", "F0", "F1", "F2", "F3"}},
		{"F0 F1 F2 F3 F4", []string{"0", "F0", "F1", "F2", "F3", "F4"}},
		{"F0 F1 F2 F3 F4 F5", []string{"F0", "F1", "F2", "F3", "F4", "F5"}},
	}

	for _, c := range cases {
		got, err := getRawFields(c.in, false)
		if err != nil {
			t.Fatalf("getRawFields(%q): %v", c.in, err)
		}
		actual := []string{got.second, got.minute, got.hour, got.dayOfMonth, got.month, got.dayOfWeek}
		for i := range c.want {
			if actual[i] != c.want[i] {
				t.Errorf("getRawFields(%q) field %d = %q, want %q (full: %v)",
					c.in, i, actual[i], c.want[i], actual)
			}
		}
	}
}

// TestFiveFieldSecondsDefault is the regression guard for the bug above:
// a standard 5-field expression must fire once per minute, not once per second.
func TestFiveFieldSecondsDefault(t *testing.T) {
	e, err := Parse("*/15 * * * *", Options{TZ: "UTC", CurrentDate: "2026-01-01T00:00:00"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := e.Fields().Second.Values(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("second field = %v, want [0]", got)
	}
	times := e.Take(3)
	if len(times) != 3 {
		t.Fatalf("got %d times, want 3", len(times))
	}
	for _, tm := range times {
		if tm.Second() != 0 {
			t.Errorf("fire at %s has nonzero seconds", tm.Format("15:04:05"))
		}
	}
	if got := times[0].Format("15:04:05"); got != "00:15:00" {
		t.Errorf("first fire = %s, want 00:15:00", got)
	}
}

func TestPredefinedExpressions(t *testing.T) {
	// @daily is six-field "0 0 0 * * *" upstream.
	e, err := Parse("@daily", Options{TZ: "UTC", CurrentDate: "2026-01-01T12:00:00"})
	if err != nil {
		t.Fatalf("parse @daily: %v", err)
	}
	times := e.Take(2)
	if len(times) != 2 {
		t.Fatalf("got %d times", len(times))
	}
	if got := times[0].Format("2006-01-02 15:04:05"); got != "2026-01-02 00:00:00" {
		t.Errorf("@daily first = %s, want 2026-01-02 00:00:00", got)
	}
}

func TestDayOfWeekModulo(t *testing.T) {
	// "7" normalizes to 0 (Sunday).
	e, err := Parse("0 0 * * 7", Options{TZ: "UTC", CurrentDate: "2026-01-01T00:00:00"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	vals := e.Fields().DayOfWeek.Values()
	if len(vals) != 1 || vals[0] != 0 {
		t.Errorf("dow values = %v, want [0]", vals)
	}
}

func TestDayOfWeekRangeZeroSeven(t *testing.T) {
	// "0-7" hits createRange's max%7==0 pre-push, so 0 appears once at the
	// front and 7 is also present as a raw value.
	e, err := Parse("0 0 * * 0-7", Options{TZ: "UTC", CurrentDate: "2026-01-01T00:00:00"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	vals := e.Fields().DayOfWeek.Values()
	// Expect 0..7 with 0 present once (the pre-push dedups against the loop).
	if len(vals) != 8 {
		t.Errorf("dow 0-7 values = %v, want 8 entries", vals)
	}
}

func TestStepFromValueExpandsToMax(t *testing.T) {
	// "5/10" is rewritten to "5-59/10" for minutes, not "just 5".
	e, err := Parse("0 5/10 * * * *", Options{TZ: "UTC", CurrentDate: "2026-01-01T00:00:00"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []int{5, 15, 25, 35, 45, 55}
	got := e.Fields().Minute.Values()
	if len(got) != len(want) {
		t.Fatalf("minute values = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("minute values = %v, want %v", got, want)
		}
	}
}

func TestAliases(t *testing.T) {
	e, err := Parse("0 0 * JAN-mar MON", Options{TZ: "UTC", CurrentDate: "2026-01-01T00:00:00"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	months := e.Fields().Month.Values()
	if len(months) != 3 || months[0] != 1 || months[2] != 3 {
		t.Errorf("months = %v, want [1 2 3]", months)
	}
	dow := e.Fields().DayOfWeek.Values()
	if len(dow) != 1 || dow[0] != 1 {
		t.Errorf("dow = %v, want [1]", dow)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		expr    string
		wantSub string
	}{
		{"0 0 0 0 0 0 0", "too many fields"},
		{"0 0 0 * * 8", "Constraint error"},
		{"0 0 25 * * *", "Constraint error"},
		{"0 0 * * * FOO", "cannot resolve alias"},
		{"0 0 * * * 1#6", "occurrence number"},
		{"0 0 * * * 1-3#2", "incompatible"},
	}
	for _, c := range cases {
		_, err := Parse(c.expr, Options{TZ: "UTC"})
		if err == nil {
			t.Errorf("Parse(%q) succeeded, want error containing %q", c.expr, c.wantSub)
			continue
		}
		if !strings.Contains(err.Error(), c.wantSub) {
			t.Errorf("Parse(%q) error = %q, want substring %q", c.expr, err.Error(), c.wantSub)
		}
	}
}
