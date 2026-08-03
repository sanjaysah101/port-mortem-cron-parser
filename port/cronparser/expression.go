package cronparser

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// Error messages, matching upstream's exported constants verbatim so callers
// that compare strings behave identically.
const (
	TimeSpanOutOfBoundsErrorMessage = "Out of the time span range"
	LoopsLimitExceededErrorMessage  = "Invalid expression, loop limit exceeded"
)

// loopLimit mirrors upstream's LOOP_LIMIT.
const loopLimit = 10000

// ErrTimeSpanOutOfBounds and ErrLoopLimitExceeded allow errors.Is checks in
// addition to the upstream-compatible messages.
var (
	ErrTimeSpanOutOfBounds = errors.New(TimeSpanOutOfBoundsErrorMessage)
	ErrLoopLimitExceeded   = errors.New(LoopsLimitExceededErrorMessage)
)

// Expression is a parsed cron expression with an iteration cursor.
//
// Upstream: CronExpression.
type Expression struct {
	fields     *FieldCollection
	expression string
	opts       Options
	loc        *time.Location

	currentDate *cronDate
	startDate   *cronDate
	endDate     *cronDate

	initialDate *cronDate // for Reset()

	dstTransitionDayKey string
	isDstTransitionDay  bool
	hasDstKey           bool
}

func newExpression(fields *FieldCollection, expression string, opts Options) (*Expression, error) {
	// Upstream: `new CronDate(timestamp, tz)` passes `{zone: tz}` to luxon, and
	// luxon falls back to the SYSTEM zone when tz is undefined. So an expression
	// parsed without a tz option is interpreted in local time, not UTC.
	//
	// Defaulting to UTC here made 91 of 130 recorded upstream test call sites
	// diverge by exactly the host's offset (5h30m on the machine that recorded
	// the trace). This is also why upstream's own suite only passes under
	// `TZ=UTC` — see DECISIONS.md D7.
	loc := time.Local
	if opts.TZ != "" {
		l, err := time.LoadLocation(opts.TZ)
		if err != nil {
			return nil, fmt.Errorf("invalid timezone %q: %w", opts.TZ, err)
		}
		loc = l
	}

	e := &Expression{
		fields:     fields,
		expression: expression,
		opts:       opts,
		loc:        loc,
	}

	if opts.StartDate != "" {
		t, err := parseDateInput(opts.StartDate, loc)
		if err != nil {
			return nil, err
		}
		e.startDate = newCronDate(t, loc)
	}
	if opts.EndDate != "" {
		t, err := parseDateInput(opts.EndDate, loc)
		if err != nil {
			return nil, err
		}
		e.endDate = newCronDate(t, loc)
	}

	// Upstream: currentDate falls back to startDate, then is clamped into
	// [startDate, endDate].
	currentValue := opts.CurrentDate
	if currentValue == "" {
		currentValue = opts.StartDate
	}

	var cur *cronDate
	if currentValue != "" {
		t, err := parseDateInput(currentValue, loc)
		if err != nil {
			return nil, err
		}
		cur = newCronDate(t, loc)
		if e.startDate != nil && cur.UnixMilli() < e.startDate.UnixMilli() {
			cur = e.startDate.clone()
		} else if e.endDate != nil && cur.UnixMilli() > e.endDate.UnixMilli() {
			cur = e.endDate.clone()
		}
	} else {
		cur = newCronDate(time.Now(), loc)
	}

	e.currentDate = cur
	e.initialDate = cur.clone()
	return e, nil
}

// Fields returns the parsed fields.
func (e *Expression) Fields() *FieldCollection { return e.fields }

// String returns the original expression text.
//
// Upstream: CronExpression.toString.
func (e *Expression) String() string {
	if e.expression != "" {
		return e.expression
	}
	return e.fields.Stringify(true)
}

// Stringify renders the fields back to expression text.
func (e *Expression) Stringify(includeSeconds bool) string {
	return e.fields.Stringify(includeSeconds)
}

// Next advances the cursor and returns the next scheduled time.
func (e *Expression) Next() (time.Time, error) {
	d, err := e.findSchedule(false)
	if err != nil {
		return time.Time{}, err
	}
	return d.Time(), nil
}

// Prev rewinds the cursor and returns the previous scheduled time.
func (e *Expression) Prev() (time.Time, error) {
	d, err := e.findSchedule(true)
	if err != nil {
		return time.Time{}, err
	}
	return d.Time(), nil
}

// HasNext reports whether a next occurrence exists, leaving the cursor
// unchanged.
//
// Upstream: CronExpression.hasNext, which restores #currentDate in a finally
// block.
func (e *Expression) HasNext() bool {
	saved := e.currentDate
	defer func() { e.currentDate = saved }()
	_, err := e.findSchedule(false)
	return err == nil
}

// HasPrev reports whether a previous occurrence exists.
func (e *Expression) HasPrev() bool {
	saved := e.currentDate
	defer func() { e.currentDate = saved }()
	_, err := e.findSchedule(true)
	return err == nil
}

// Take returns up to limit occurrences, forward for positive, backward for
// negative. It stops early and returns what it has when iteration errors.
//
// Upstream: CronExpression.take.
func (e *Expression) Take(limit int) []time.Time {
	var items []time.Time
	if limit >= 0 {
		for i := 0; i < limit; i++ {
			t, err := e.Next()
			if err != nil {
				return items
			}
			items = append(items, t)
		}
		return items
	}
	for i := 0; i > limit; i-- {
		t, err := e.Prev()
		if err != nil {
			return items
		}
		items = append(items, t)
	}
	return items
}

// Reset returns the cursor to the initial date, or to newDate when non-zero.
func (e *Expression) Reset(newDate ...time.Time) {
	if len(newDate) > 0 && !newDate[0].IsZero() {
		e.currentDate = newCronDate(newDate[0], e.loc)
		return
	}
	e.currentDate = e.initialDate.clone()
}

// IncludesDate reports whether t satisfies every field of the expression.
//
// Upstream: CronExpression.includesDate.
func (e *Expression) IncludesDate(t time.Time) bool {
	d := newCronDate(t, e.loc)

	if !e.fields.Second.Matches(d.Second()) ||
		!e.fields.Minute.Matches(d.Minute()) ||
		!e.fields.Hour.Matches(d.Hour()) ||
		!e.fields.Month.Matches(d.Month()+1) {
		return false
	}
	return e.matchDayOfMonth(d)
}

// checkDstTransition reports whether the date's calendar day contains a UTC
// offset change, caching per day.
//
// Upstream: CronExpression.#checkDstTransition.
func (e *Expression) checkDstTransition(d *cronDate) bool {
	key := fmt.Sprintf("%d-%d-%d", d.Year(), d.Month()+1, d.Day())
	if e.hasDstKey && e.dstTransitionDayKey == key {
		return e.isDstTransitionDay
	}

	startOfDay := d.clone()
	startOfDay.SetStartOfDay()
	endOfDay := d.clone()
	endOfDay.SetEndOfDay()

	e.dstTransitionDayKey = key
	e.hasDstKey = true
	e.isDstTransitionDay = startOfDay.UTCOffsetMinutes() != endOfDay.UTCOffsetMinutes()
	return e.isDstTransitionDay
}

// matchDayOfMonth implements the POSIX day-of-month / day-of-week rules.
//
// Upstream: CronExpression.#matchDayOfMonth. The three rules are transcribed in
// order; rule 1 is the OR semantics that FINDINGS.md #3 verified against
// robfig/cron.
func (e *Expression) matchDayOfMonth(d *cronDate) bool {
	isDomWildcard := e.fields.DayOfMonth.IsWildcard()
	isRestrictedDom := !isDomWildcard
	isDowWildcard := e.fields.DayOfWeek.IsWildcard()
	isRestrictedDow := !isDowWildcard

	matchedDOM := e.fields.DayOfMonth.Matches(d.Day()) ||
		(e.fields.DayOfMonth.HasLastChar() && d.IsLastDayOfMonth())

	nthDay := e.fields.DayOfWeek.NthDayOfWeek()
	matchedDOW := (e.fields.DayOfWeek.Matches(d.DayOfWeek()) && isNthWeekdayOfMonthMatch(nthDay, d)) ||
		(e.fields.DayOfWeek.HasLastChar() && isLastWeekdayOfMonthMatch(e.fields.DayOfWeek, d))

	// Rule 1: both restricted -> either may match (POSIX OR).
	if isRestrictedDom && isRestrictedDow && (matchedDOM || matchedDOW) {
		return true
	}
	// Rule 2: dom restricted, dow not -> dom must match.
	if matchedDOM && !isRestrictedDow {
		return true
	}
	// Rule 3: dom wildcard, dow restricted and matching.
	if isDomWildcard && !isDowWildcard && matchedDOW {
		return true
	}
	return false
}

// isNthWeekdayOfMonthMatch implements the "#n" constraint.
//
// Upstream: Math.ceil(getDate() / 7) === nthDay.
func isNthWeekdayOfMonthMatch(nthDay int, d *cronDate) bool {
	if nthDay <= 0 {
		return true
	}
	return int(math.Ceil(float64(d.Day())/7.0)) == nthDay
}

// isLastWeekdayOfMonthMatch implements the "L" suffix on dayOfWeek.
//
// Upstream parses the FIRST CHARACTER of each value as an int and mods by 7:
//
//	expressions.some((e) => day === parseInt(e.toString().charAt(0), 10) % 7)
//
// Taking only charAt(0) means a two-digit value would be misread; that quirk is
// preserved because values here are single digits after %7 normalization.
func isLastWeekdayOfMonthMatch(f *Field, d *cronDate) bool {
	if !d.IsLastWeekdayOfMonth() {
		return false
	}
	day := d.DayOfWeek()
	for _, s := range f.sortedValues() {
		if len(s) == 0 {
			continue
		}
		c := s[0]
		if c < '0' || c > '9' {
			continue
		}
		if day == int(c-'0')%7 {
			return true
		}
	}
	return false
}

// moveToNextSecond advances to the next allowed second, rolling the minute when
// none remains.
//
// Upstream: CronExpression.#moveToNextSecond.
func (e *Expression) moveToNextSecond(d *cronDate, op DateMathOp, reverse bool) {
	next, ok := e.fields.Second.FindNearestValue(d.Second(), reverse)
	if ok {
		d.SetSeconds(next)
		return
	}
	d.ApplyDateOperation(op, UnitTimeMinute, len(e.fields.Hour.Values()))
	d.SetSeconds(e.fields.Second.minOrMax(reverse))
}

// moveToNextMinute advances to the next allowed minute, rolling the hour when
// none remains.
//
// Upstream: CronExpression.#moveToNextMinute.
func (e *Expression) moveToNextMinute(d *cronDate, op DateMathOp, reverse bool) {
	next, ok := e.fields.Minute.FindNearestValue(d.Minute(), reverse)
	if ok {
		d.SetMinutes(next)
		d.SetSeconds(e.fields.Second.minOrMax(reverse))
		return
	}
	d.ApplyDateOperation(op, UnitTimeHour, len(e.fields.Hour.Values()))
	d.SetMinutes(e.fields.Minute.minOrMax(reverse))
	d.SetSeconds(e.fields.Second.minOrMax(reverse))
}

// matchHour reports whether the current hour satisfies the expression, mutating
// the date toward a candidate when it does not.
//
// Upstream: CronExpression.#matchHour. This is the most delicate function in the
// port: it carries both DST hint branches AND the fast-path/step-by-step
// selection that depends on checkDstTransition.
func (e *Expression) matchHour(d *cronDate, op DateMathOp, reverse bool) bool {
	hours := e.fields.Hour.Values()
	currentHour := d.Hour()
	isMatch := e.fields.Hour.Matches(currentHour)
	isDstEnd := d.dstEnd != nil && *d.dstEnd == currentHour

	// DST start: accept the next existing hour when the scheduled hour was
	// skipped by a spring-forward.
	if d.dstStart != nil && *d.dstStart == currentHour-1 {
		if e.fields.Hour.Matches(*d.dstStart) {
			return true
		}
	}

	// DST end: when moving forward, do not emit the repeated hour twice.
	// This branch is why cron-parser fires a literal-hour daily job ONCE on
	// fall-back where robfig/cron fires it twice (FINDINGS.md #2b).
	if isDstEnd && !reverse {
		d.dstEnd = nil
		d.ApplyDateOperation(OpAdd, UnitTimeHour, len(hours))
		return false
	}

	if isMatch {
		return true
	}

	d.dstStart = nil
	nextHour, ok := e.fields.Hour.FindNearestValue(currentHour, reverse)
	if !ok {
		d.ApplyDateOperation(op, UnitTimeDay, len(hours))
		return false
	}

	// Fast path: jump straight to the matching hour. On a DST transition day
	// that can land on a nonexistent or repeated local time, so step
	// hour-by-hour instead to keep the dstStart/dstEnd hints correct.
	if e.checkDstTransition(d) {
		steps := nextHour - currentHour
		if reverse {
			steps = currentHour - nextHour
		}
		for i := 0; i < steps; i++ {
			d.ApplyDateOperation(op, UnitTimeHour, len(hours))
			// Overshoot protection: a spring-forward step can jump two
			// wall-clock hours.
			if !reverse && d.Hour() >= nextHour {
				break
			}
			if reverse && d.Hour() <= nextHour {
				break
			}
		}
	} else {
		d.SetHours(nextHour)
	}
	d.SetMinutes(e.fields.Minute.minOrMax(reverse))
	d.SetSeconds(e.fields.Second.minOrMax(reverse))
	return false
}

func (e *Expression) validateTimeSpan(d *cronDate) error {
	if e.startDate == nil && e.endDate == nil {
		return nil
	}
	now := d.UnixMilli()
	if e.startDate != nil && now < e.startDate.UnixMilli() {
		return ErrTimeSpanOutOfBounds
	}
	if e.endDate != nil && now > e.endDate.UnixMilli() {
		return ErrTimeSpanOutOfBounds
	}
	return nil
}

// findSchedule is the iteration loop.
//
// Upstream: CronExpression.#findSchedule. The structure is a mutate-and-retry
// loop with a hard step cap; each `continue` re-tests every field from the top,
// so field check ORDER is observable. Preserve it exactly.
func (e *Expression) findSchedule(reverse bool) (*cronDate, error) {
	op := OpAdd
	if reverse {
		op = OpSubtract
	}

	currentDate := e.currentDate.clone()
	startTimestamp := currentDate.UnixMilli()

	if currentDate.Millisecond() > 0 {
		currentDate.SetMilliseconds(0)
		if !reverse {
			currentDate.ApplyDateOperation(OpAdd, UnitTimeSecond, len(e.fields.Hour.Values()))
		}
	}

	stepCount := 0
	for {
		stepCount++
		if stepCount >= loopLimit {
			return nil, ErrLoopLimitExceeded
		}

		if err := e.validateTimeSpan(currentDate); err != nil {
			return nil, err
		}

		if !e.matchDayOfMonth(currentDate) {
			currentDate.ApplyDateOperation(op, UnitTimeDay, len(e.fields.Hour.Values()))
			continue
		}
		if !e.fields.Month.Matches(currentDate.Month() + 1) {
			currentDate.ApplyDateOperation(op, UnitTimeMonth, len(e.fields.Hour.Values()))
			continue
		}
		if !e.matchHour(currentDate, op, reverse) {
			continue
		}
		if !e.fields.Minute.Matches(currentDate.Minute()) {
			e.moveToNextMinute(currentDate, op, reverse)
			continue
		}
		if !e.fields.Second.Matches(currentDate.Second()) {
			e.moveToNextSecond(currentDate, op, reverse)
			continue
		}

		// Iteration is exclusive of the starting instant.
		if startTimestamp == currentDate.UnixMilli() {
			currentDate.ApplyDateOperation(op, UnitTimeSecond, len(e.fields.Hour.Values()))
			continue
		}
		break
	}

	e.currentDate = currentDate
	return currentDate, nil
}
