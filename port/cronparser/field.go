package cronparser

import (
	"fmt"
	"sort"
	"strings"
)

// Unit identifies a cron field position.
type Unit int

const (
	UnitSecond Unit = iota
	UnitMinute
	UnitHour
	UnitDayOfMonth
	UnitMonth
	UnitDayOfWeek
)

func (u Unit) String() string {
	switch u {
	case UnitSecond:
		return "Second"
	case UnitMinute:
		return "Minute"
	case UnitHour:
		return "Hour"
	case UnitDayOfMonth:
		return "DayOfMonth"
	case UnitMonth:
		return "Month"
	case UnitDayOfWeek:
		return "DayOfWeek"
	}
	return "Unknown"
}

// constraints mirrors CronConstraints: the legal numeric range plus any
// special characters the field accepts (L, ?, W...).
type constraints struct {
	min   int
	max   int
	chars []string
}

var unitConstraints = map[Unit]constraints{
	UnitSecond:     {0, 59, nil},
	UnitMinute:     {0, 59, nil},
	UnitHour:       {0, 23, nil},
	UnitDayOfMonth: {1, 31, []string{"L"}},
	UnitMonth:      {1, 12, nil},
	UnitDayOfWeek:  {0, 7, []string{"L"}},
}

// Field is one parsed cron field.
//
// Upstream stores values as a heterogeneous (number | string)[] so that the
// literal "L" can live alongside numbers. Go has no such union, so numeric and
// character values are held separately and recombined only where upstream
// observably depends on the mixed ordering (see sortedValues).
type Field struct {
	unit     Unit
	values   []int    // numeric values, ascending, deduplicated
	chars    []string // special char values present, e.g. ["L"]
	rawValue string
	wildcard bool

	hasLastChar  bool
	hasQuestion  bool
	nthDayOfWeek int // 0 = no nth constraint
}

// Values returns the numeric values, ascending.
func (f *Field) Values() []int { return f.values }

// IsWildcard reports whether the field was written as "*" or "?".
func (f *Field) IsWildcard() bool { return f.wildcard }

// HasLastChar reports whether the field contains "L".
func (f *Field) HasLastChar() bool { return f.hasLastChar }

// NthDayOfWeek returns the "#n" occurrence constraint, or 0 if absent.
func (f *Field) NthDayOfWeek() int { return f.nthDayOfWeek }

// Raw returns the field text as written in the expression.
func (f *Field) Raw() string { return f.rawValue }

// Matches reports whether v is in the field's numeric values.
//
// Upstream: CronExpression.#matchSchedule.
func (f *Field) Matches(v int) bool {
	for _, x := range f.values {
		if x == v {
			return true
		}
	}
	return false
}

// FindNearestValue returns the next value strictly greater than current, or
// when reverse the previous value strictly less. The bool reports whether one
// exists; upstream returns null.
//
// Upstream: CronField.findNearestValueInList.
func (f *Field) FindNearestValue(current int, reverse bool) (int, bool) {
	if reverse {
		for i := len(f.values) - 1; i >= 0; i-- {
			if f.values[i] < current {
				return f.values[i], true
			}
		}
		return 0, false
	}
	for i := 0; i < len(f.values); i++ {
		if f.values[i] > current {
			return f.values[i], true
		}
	}
	return 0, false
}

// min/max returns the first or last numeric value.
//
// Upstream: CronExpression.#getMinOrMax, which indexes values[0] or
// values[len-1] on the already-sorted array.
func (f *Field) minOrMax(reverse bool) int {
	if len(f.values) == 0 {
		return 0
	}
	if reverse {
		return f.values[len(f.values)-1]
	}
	return f.values[0]
}

// sortedValues reproduces upstream's CronField.sorter ordering: numbers
// ascending first, then strings by locale compare. Only needed for stringify
// and for the L-matching path that reads values as text.
func (f *Field) sortedValues() []string {
	out := make([]string, 0, len(f.values)+len(f.chars))
	for _, v := range f.values {
		out = append(out, fmt.Sprintf("%d", v))
	}
	chars := append([]string(nil), f.chars...)
	sort.Strings(chars)
	out = append(out, chars...)
	return out
}

// newField builds a Field, applying upstream's dedup-and-sort and its
// wildcard/L/? detection.
func newField(unit Unit, values []int, chars []string, raw string, nth int) (*Field, error) {
	if len(values) == 0 && len(chars) == 0 {
		return nil, fmt.Errorf("%s Validation error, values contains no values", unit)
	}

	c := unitConstraints[unit]

	// Upstream sorts and keeps duplicates, then validate() rejects them.
	// Detect duplicates before deduplicating so the same error surfaces.
	seen := map[int]bool{}
	for _, v := range values {
		if seen[v] {
			return nil, fmt.Errorf("%s Validation error, duplicate values found: %d", unit, v)
		}
		seen[v] = true
	}

	for _, v := range values {
		if v < c.min || v > c.max {
			charsStr := ""
			if len(c.chars) > 0 {
				charsStr = " or chars " + strings.Join(c.chars, "")
			}
			return nil, fmt.Errorf("%s Validation error, got value %d expected range %d-%d%s",
				unit, v, c.min, c.max, charsStr)
		}
	}

	sorted := append([]int(nil), values...)
	sort.Ints(sorted)

	f := &Field{
		unit:         unit,
		values:       sorted,
		chars:        chars,
		rawValue:     raw,
		nthDayOfWeek: nth,
	}

	f.hasLastChar = strings.Contains(raw, "L")
	f.hasQuestion = strings.Contains(raw, "?")
	f.wildcard = raw == "*" || raw == "?"

	return f, nil
}

// FieldCollection is the six parsed fields of an expression.
type FieldCollection struct {
	Second     *Field
	Minute     *Field
	Hour       *Field
	DayOfMonth *Field
	Month      *Field
	DayOfWeek  *Field
}

// Stringify renders the expression back to text.
//
// Upstream: CronFieldCollection.stringify.
func (fc *FieldCollection) Stringify(includeSeconds bool) string {
	parts := []string{}
	if includeSeconds {
		parts = append(parts, fc.Second.rawValue)
	}
	parts = append(parts,
		fc.Minute.rawValue,
		fc.Hour.rawValue,
		fc.DayOfMonth.rawValue,
		fc.Month.rawValue,
		fc.DayOfWeek.rawValue,
	)
	return strings.Join(parts, " ")
}
