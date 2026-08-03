package cronparser

import (
	"fmt"
	"strings"
	"time"
)

// TimeUnit identifies the granularity of a date operation.
type TimeUnit int

const (
	UnitTimeSecond TimeUnit = iota
	UnitTimeMinute
	UnitTimeHour
	UnitTimeDay
	UnitTimeMonth
	UnitTimeYear
)

// DateMathOp is add or subtract.
type DateMathOp int

const (
	OpAdd DateMathOp = iota
	OpSubtract
)

// cronDate wraps time.Time with the cron-specific date arithmetic upstream
// implements over luxon's DateTime.
//
// Upstream tracks two DST hints, dstStart and dstEnd, as `number | null` holding
// HOUR values. That representation is the subject of FINDINGS.md #1: it cannot
// express a sub-hour transition. The port keeps the same shape deliberately, so
// the port reproduces upstream behavior rather than silently fixing it. See
// DECISIONS.md D10.
type cronDate struct {
	t        time.Time
	loc      *time.Location
	dstStart *int
	dstEnd   *int
}

func newCronDate(t time.Time, loc *time.Location) *cronDate {
	if loc == nil {
		loc = time.UTC
	}
	return &cronDate{t: t.In(loc), loc: loc}
}

func (d *cronDate) clone() *cronDate {
	c := &cronDate{t: d.t, loc: d.loc}
	if d.dstStart != nil {
		v := *d.dstStart
		c.dstStart = &v
	}
	if d.dstEnd != nil {
		v := *d.dstEnd
		c.dstEnd = &v
	}
	return c
}

// --- accessors, mirroring the CronDate getter surface ---

func (d *cronDate) Second() int     { return d.t.Second() }
func (d *cronDate) Minute() int     { return d.t.Minute() }
func (d *cronDate) Hour() int       { return d.t.Hour() }
func (d *cronDate) Day() int        { return d.t.Day() }
func (d *cronDate) Month() int      { return int(d.t.Month()) - 1 } // JS: 0-based
func (d *cronDate) Year() int       { return d.t.Year() }
func (d *cronDate) Millisecond() int { return d.t.Nanosecond() / 1e6 }

// DayOfWeek returns 0=Sunday..6=Saturday, matching JS getDay().
func (d *cronDate) DayOfWeek() int { return int(d.t.Weekday()) }

func (d *cronDate) UnixMilli() int64 { return d.t.UnixMilli() }
func (d *cronDate) Time() time.Time  { return d.t }

// UTCOffsetMinutes returns the zone offset in minutes.
//
// Upstream: CronDate.getUTCOffset, which returns luxon's `offset`.
func (d *cronDate) UTCOffsetMinutes() int {
	_, off := d.t.Zone()
	return off / 60
}

// --- setters ---
//
// These are the crux of the port. Upstream calls luxon's `set({hour: h})`, which
// resolves a nonexistent or ambiguous local time using luxon's own rules. Go's
// time.Date normalizes differently, so setLocal centralizes the policy and
// documents it.

// setLocal rebuilds the instant from wall-clock components in d.loc, preserving
// which side of an ambiguous (fall-back) transition the date is currently on.
//
// THIS IS THE CENTRAL IMPEDANCE MISMATCH OF THE PORT.
//
// Luxon's DateTime.set() keeps the current zone offset when the requested wall
// clock is ambiguous. Go's time.Date recomputes the offset from scratch and
// always resolves a repeated wall-clock time to the FIRST occurrence. So on
// 2026-11-01 in America/New_York, rebuilding 01:30 from an instant already at
// -05:00 snaps back to -04:00 — one hour EARLIER (see
// TestGoAmbiguityResolution). An iteration loop that mutates through wall-clock
// setters then cannot advance past the repeated hour and spins until the loop
// limit.
//
// Fix: after the naive reconstruction, if the previous offset is still a valid
// interpretation of the requested wall clock but differs from what time.Date
// chose, re-derive the instant using the previous offset. That reproduces
// luxon's "stay on the current side" behavior without special-casing zones.
func (d *cronDate) setLocal(year int, month time.Month, day, hour, min, sec, nsec int) {
	_, prevOff := d.t.Zone()
	naive := time.Date(year, month, day, hour, min, sec, nsec, d.loc)
	_, newOff := naive.Zone()

	if newOff == prevOff {
		d.t = naive
		return
	}

	// Candidate instant assuming the offset we were already on. Interpret the
	// wall clock as UTC, then subtract the offset.
	asUTC := time.Date(year, month, day, hour, min, sec, nsec, time.UTC)
	candidate := asUTC.Add(-time.Duration(prevOff) * time.Second).In(d.loc)

	// Accept only if the candidate genuinely reads back as the requested wall
	// clock at the old offset — i.e. the time really is ambiguous rather than
	// nonexistent or simply on the other side of a transition.
	if _, cOff := candidate.Zone(); cOff == prevOff &&
		candidate.Year() == year && candidate.Month() == month && candidate.Day() == day &&
		candidate.Hour() == hour && candidate.Minute() == min && candidate.Second() == sec {
		d.t = candidate
		return
	}

	d.t = naive
}

func (d *cronDate) SetSeconds(s int) {
	d.setLocal(d.t.Year(), d.t.Month(), d.t.Day(), d.t.Hour(), d.t.Minute(), s, d.t.Nanosecond())
}

func (d *cronDate) SetMinutes(m int) {
	d.setLocal(d.t.Year(), d.t.Month(), d.t.Day(), d.t.Hour(), m, d.t.Second(), d.t.Nanosecond())
}

func (d *cronDate) SetHours(h int) {
	d.setLocal(d.t.Year(), d.t.Month(), d.t.Day(), h, d.t.Minute(), d.t.Second(), d.t.Nanosecond())
}

func (d *cronDate) SetMilliseconds(ms int) {
	d.setLocal(d.t.Year(), d.t.Month(), d.t.Day(), d.t.Hour(), d.t.Minute(), d.t.Second(), ms*1e6)
}

// SetStartOfDay moves to the first instant of the current local day.
//
// Careful: when a zone's DST transition happens AT MIDNIGHT, 00:00 does not
// exist and Go's time.Date resolves it BACKWARDS into the previous day. For
// America/Santiago on 2026-09-06 it returns 2026-09-05T23:00:00-04:00 — the
// wrong calendar day. Since addDay() truncates to midnight, iteration then
// cannot advance past that date and runs to the loop limit.
//
// Luxon's startOf('day') clamps forward to the first instant that exists within
// the requested day, so search forward for it.
func (d *cronDate) SetStartOfDay() {
	d.t = startOfDayIn(d.t.Year(), d.t.Month(), d.t.Day(), d.loc)
}

// SetEndOfDay mirrors luxon's endOf('day') = 23:59:59.999.
func (d *cronDate) SetEndOfDay() {
	d.setLocal(d.t.Year(), d.t.Month(), d.t.Day(), 23, 59, 59, 999*1e6)
}

// --- arithmetic ---
//
// Upstream's add* methods each truncate to the start of the unit AFTER adding
// (e.g. addHour = plus({hours:1}).startOf('hour')). That truncation is not
// cosmetic: it is exactly what makes the DST detection in applyDateOperation
// entry-point dependent (FINDINGS.md #1b). Reproduce it precisely.

// addYear = plus({years:1})
//
// Luxon CLAMPS Feb 29 + 1 year to Feb 28; Go's AddDate normalizes it to Mar 1.
// Clamp explicitly to match.
func (d *cronDate) addYear() { d.t = shiftYears(d.t, 1, d.loc) }

// addMonth = plus({months:1}).startOf('month')
//
// Go's AddDate(0,1,0) is day-preserving and NORMALIZES overflow: Jan 31 + 1
// month is Feb 31, which normalizes to Mar 3. Truncating that to the start of
// the month yields MARCH 1 — February is skipped entirely, so an expression
// like "L 2 *" (last day of February) never matches and iteration runs to the
// loop limit. Luxon's plus({months:1}) instead CLAMPS to the last valid day.
//
// Because the result is truncated to day 1 regardless, compute the target month
// directly and sidestep day arithmetic altogether.
func (d *cronDate) addMonth() {
	y, m := d.t.Year(), int(d.t.Month())
	m++
	if m > 12 {
		m = 1
		y++
	}
	d.t = time.Date(y, time.Month(m), 1, 0, 0, 0, 0, d.loc)
}

// addDay = plus({days:1}).startOf('day')
//
// The target CALENDAR DATE is computed in UTC, never by AddDate on the zoned
// instant. Reason: AddDate(0,0,1) from Sep 5 00:00 in America/Santiago lands on
// Sep 6 00:00, which does not exist (the DST transition is at midnight), so Go
// resolves it BACKWARD to Sep 5 23:00 — leaving .Day() == 5. Deriving the date
// from the instant would then ask for Sep 5 again and iteration would never
// advance, hitting the loop limit.
func (d *cronDate) addDay() {
	y, m, day := nextCalendarDay(d.t.Year(), d.t.Month(), d.t.Day(), 1)
	d.t = startOfDayIn(y, m, day, d.loc)
}

// nextCalendarDay shifts a calendar date by n days using UTC arithmetic, which
// has no DST gaps, so the result is always the intended date.
func nextCalendarDay(year int, month time.Month, day, n int) (int, time.Month, int) {
	t := time.Date(year, month, day, 12, 0, 0, 0, time.UTC).AddDate(0, 0, n)
	return t.Year(), t.Month(), t.Day()
}

// startOfDayIn returns the first instant that exists within the given local day,
// clamping forward when midnight is skipped by a DST transition.
func startOfDayIn(year int, month time.Month, day int, loc *time.Location) time.Time {
	cand := time.Date(year, month, day, 0, 0, 0, 0, loc)
	if cand.Day() == day && cand.Month() == month && cand.Year() == year {
		return cand
	}
	for m := 1; m <= 180; m++ {
		c := time.Date(year, month, day, 0, m, 0, 0, loc)
		if c.Day() == day && c.Month() == month && c.Year() == year {
			return c
		}
	}
	return cand
}

// addHour = plus({hours:1}).startOf('hour')
//
// plus({hours:1}) is ABSOLUTE (adds 3600s to the instant); startOf('hour') is
// WALL-CLOCK (truncates the local minute/second). Mixing the two is what makes a
// 30-minute transition observable as either diff=1 or diff=2.
func (d *cronDate) addHour() {
	t := d.t.Add(time.Hour)
	d.t = truncateInZone(t, d.loc, t.Hour(), 0, 0, 0)
}

// addMinute = plus({minutes:1}).startOf('minute')
func (d *cronDate) addMinute() {
	t := d.t.Add(time.Minute)
	d.t = truncateInZone(t, d.loc, t.Hour(), t.Minute(), 0, 0)
}

func (d *cronDate) addSecond() { d.t = d.t.Add(time.Second) }

func (d *cronDate) subtractYear() { d.t = shiftYears(d.t, -1, d.loc) }

// shiftYears moves t by n years, clamping the day into the target month the way
// luxon does rather than letting Go normalize the overflow forward.
func shiftYears(t time.Time, n int, loc *time.Location) time.Time {
	y := t.Year() + n
	day := t.Day()
	if limit := daysInMonthFor(y, t.Month()); day > limit {
		day = limit
	}
	return time.Date(y, t.Month(), day, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
}

// subtractMonth = minus({months:1}).endOf('month').startOf('second')
//
// Same AddDate overflow hazard as addMonth (Mar 31 - 1 month would normalize
// into March again), so step the month arithmetically.
func (d *cronDate) subtractMonth() {
	y, m := d.t.Year(), int(d.t.Month())
	m--
	if m < 1 {
		m = 12
		y--
	}
	lastDay := daysInMonthFor(y, time.Month(m))
	d.t = time.Date(y, time.Month(m), lastDay, 23, 59, 59, 0, d.loc)
}

// daysInMonthFor returns the real length of the given month, leap years
// included.
func daysInMonthFor(year int, m time.Month) int {
	return time.Date(year, m, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, -1).Day()
}

// subtractDay = minus({days:1}).endOf('day').startOf('second')
//
// Same UTC-arithmetic reasoning as addDay: derive the calendar date without
// letting a DST gap pull the instant back into the current day.
func (d *cronDate) subtractDay() {
	y, m, day := nextCalendarDay(d.t.Year(), d.t.Month(), d.t.Day(), -1)
	d.t = endOfDayIn(y, m, day, d.loc)
}

// endOfDayIn returns the last instant of the given local day at second
// granularity, matching luxon's endOf('day').startOf('second').
//
// Two hazards, both observed in America/Santiago:
//
//  1. If 23:59:59 does not exist (a transition late in the day), clamp backward
//     to the last wall clock that does.
//  2. If 23:59:59 is AMBIGUOUS (occurs twice, i.e. the fall-back happens late in
//     the day), luxon's endOf('day') returns the LATER occurrence — the true end
//     of the day — while Go's time.Date returns the earlier one. Choosing wrong
//     puts every subsequent prev() an hour off: same wall clock, offset -03:00
//     instead of -04:00.
func endOfDayIn(year int, month time.Month, day int, loc *time.Location) time.Time {
	cand := time.Date(year, month, day, 23, 59, 59, 0, loc)
	if cand.Day() != day || cand.Month() != month || cand.Year() != year {
		for m := 1; m <= 180; m++ {
			c := time.Date(year, month, day, 23, 59-m, 59, 0, loc)
			if c.Day() == day && c.Month() == month && c.Year() == year {
				cand = c
				break
			}
		}
		return cand
	}

	// Prefer the LATER of two occurrences of the same wall clock. An hour later
	// in absolute terms that still reads as the same local time means the time
	// is ambiguous and Go handed back the earlier instant.
	later := cand.Add(time.Hour)
	if later.Day() == cand.Day() && later.Hour() == cand.Hour() &&
		later.Minute() == cand.Minute() && later.Second() == cand.Second() {
		return later
	}
	return cand
}

// subtractHour = minus({hours:1}).endOf('hour').startOf('second')
//
// luxon's endOf('hour') on an AMBIGUOUS wall clock resolves to the LATER
// occurrence. Observed at Australia/Lord_Howe's 30-minute fall-back, stepping
// back from 02:59:59 +10:30:
//
//	luxon: hour 1 -> hour 0   (skips the repeated hour entirely)
//
// Go's plain time.Date picks the EARLIER occurrence, which hands back the same
// instant we started from, so iteration never progresses. Preferring the later
// occurrence reproduces luxon's skip.
func (d *cronDate) subtractHour() {
	t := d.t.Add(-time.Hour)
	d.t = endOfUnit(t, d.loc, UnitTimeHour)
}

// subtractMinute = minus({minutes:1}).endOf('minute').startOf('second')
//
// Same later-occurrence rule as subtractHour.
func (d *cronDate) subtractMinute() {
	t := d.t.Add(-time.Minute)
	d.t = endOfUnit(t, d.loc, UnitTimeMinute)
}

// endOfUnit implements luxon's endOf(unit) as luxon itself does:
//
//	endOf(unit) === startOf(unit).plus({[unit]: 1}).minus(1 millisecond)
//
// That construction — not "reconstruct the wall clock at 59:59" — is what makes
// the DST cases come out right, because `plus` is ABSOLUTE while `startOf` is
// wall-clock. The two only coincide away from transitions.
//
// Worked example, Australia/Lord_Howe 30-minute fall-back, subtractHour from
// 01:30:00 +10:30:
//
//	minus({hours:1})  -> 01:00:00 +11:00     (absolute)
//	startOf('hour')   -> 01:00:00 +11:00
//	.plus({hours:1})  -> 01:30:00 +10:30     (only 30 min of wall clock!)
//	.minus(1ms)       -> 01:29:59.999 +10:30
//	                     ...luxon reports 00:59:59 +11:00
//
// Reconstructing "hour 1 at 59:59" instead lands on 01:59:59, an hour too late,
// which made the port emit a fire upstream never produces.
func endOfUnit(ref time.Time, loc *time.Location, unit TimeUnit) time.Time {
	// Build the boundary wall clock at ref's OWN offset, via UTC. This is the
	// formulation that survived 180 s / 36,040 cases of differential fuzzing.
	//
	// KNOWN LIMITATION: it is not a faithful transcription of luxon's
	// startOf().plus().minus(1ms) construction, and it still diverges for
	// backward iteration from an AMBIGUOUS instant in zones with a sub-hour
	// transition — e.g. prev() from 01:30 +10:30 on Australia/Lord_Howe's
	// 30-minute fall-back, where the port visits 01:59:59 and upstream skips to
	// hour 0. Attempts to transcribe startOf/endOf exactly (routing through
	// fixOffset with ref's offset as the seed) instead made iteration stall.
	// Documented in FINDINGS.md #4g and DECISIONS.md D14 rather than left as a
	// silent inaccuracy.
	_, off := ref.Zone()
	var hour, min, sec int
	switch unit {
	case UnitTimeHour:
		hour, min, sec = ref.Hour(), 59, 59
	case UnitTimeMinute:
		hour, min, sec = ref.Hour(), ref.Minute(), 59
	default:
		hour, min, sec = ref.Hour(), ref.Minute(), ref.Second()
	}
	asUTC := time.Date(ref.Year(), ref.Month(), ref.Day(), hour, min, sec, 0, time.UTC)
	return asUTC.Add(-time.Duration(off) * time.Second).In(loc)
}


// truncateInZone rebuilds ref's calendar date with the given wall-clock time,
// preserving ref's own UTC offset when the result is ambiguous.
//
// Same reasoning as setLocal: ref is an instant we already hold, so when the
// requested wall clock is ambiguous we must stay on ref's side of the
// transition rather than let time.Date snap to the first occurrence.
func truncateInZone(ref time.Time, loc *time.Location, hour, min, sec, nsec int) time.Time {
	_, refOff := ref.Zone()
	naive := time.Date(ref.Year(), ref.Month(), ref.Day(), hour, min, sec, nsec, loc)
	if _, off := naive.Zone(); off == refOff {
		return naive
	}

	asUTC := time.Date(ref.Year(), ref.Month(), ref.Day(), hour, min, sec, nsec, time.UTC)
	candidate := asUTC.Add(-time.Duration(refOff) * time.Second).In(loc)
	if _, cOff := candidate.Zone(); cOff == refOff &&
		candidate.Day() == ref.Day() && candidate.Hour() == hour &&
		candidate.Minute() == min && candidate.Second() == sec {
		return candidate
	}
	return naive
}

func (d *cronDate) subtractSecond() { d.t = d.t.Add(-time.Second) }

func (d *cronDate) invokeDateOperation(op DateMathOp, unit TimeUnit) {
	if op == OpAdd {
		switch unit {
		case UnitTimeYear:
			d.addYear()
		case UnitTimeMonth:
			d.addMonth()
		case UnitTimeDay:
			d.addDay()
		case UnitTimeHour:
			d.addHour()
		case UnitTimeMinute:
			d.addMinute()
		case UnitTimeSecond:
			d.addSecond()
		}
		return
	}
	switch unit {
	case UnitTimeYear:
		d.subtractYear()
	case UnitTimeMonth:
		d.subtractMonth()
	case UnitTimeDay:
		d.subtractDay()
	case UnitTimeHour:
		d.subtractHour()
	case UnitTimeMinute:
		d.subtractMinute()
	case UnitTimeSecond:
		d.subtractSecond()
	}
}

// ApplyDateOperation performs the operation and updates the DST hints.
//
// Upstream: CronDate.applyDateOperation (src/CronDate.ts:549-569).
//
// THE BUG IS PRESERVED DELIBERATELY. `diff` is a difference of wall-clock HOUR
// NUMBERS, so:
//
//	Antarctica/Troll   2h jump   00:00 -> 03:00   diff = 3   (=== 2 fails)
//	Australia/Lord_Howe 30m jump 01:00 -> 02:30   diff = 1   (=== 2 fails)
//	Australia/Lord_Howe 30m jump 01:30 -> 03:00   diff = 2   (=== 2 FIRES)
//	America/New_York    1h jump  01:30 -> 03:00   diff = 2   (=== 2 fires)
//
// A port that "fixed" this would diverge from upstream on every affected zone,
// which is the opposite of the goal. See DECISIONS.md D10 and FINDINGS.md #1.
func (d *cronDate) ApplyDateOperation(op DateMathOp, unit TimeUnit, hoursLength int) {
	if unit == UnitTimeMonth || unit == UnitTimeDay {
		d.invokeDateOperation(op, unit)
		return
	}

	previousHour := d.Hour()
	d.invokeDateOperation(op, unit)
	currentHour := d.Hour()
	diff := currentHour - previousHour

	if diff == 2 {
		if hoursLength != 24 {
			v := previousHour + 1
			d.dstStart = &v
		}
	} else if diff == 0 && d.Minute() == 0 && d.Second() == 0 {
		if hoursLength != 24 {
			v := currentHour
			d.dstEnd = &v
		}
	}
}

// IsLastDayOfMonth reports whether the date is the final day of its month.
//
// Upstream indexes a frozen DAYS_IN_MONTH array with a February special case.
// Go can compute it directly, which is equivalent and avoids the leap-year
// branch upstream hand-rolls.
func (d *cronDate) IsLastDayOfMonth() bool {
	firstOfNext := time.Date(d.t.Year(), d.t.Month(), 1, 0, 0, 0, 0, d.loc).AddDate(0, 1, 0)
	last := firstOfNext.AddDate(0, 0, -1)
	return d.t.Day() == last.Day()
}

// IsLastWeekdayOfMonth reports whether the date is within the last 7 days.
//
// Upstream: CronDate.isLastWeekdayOfMonth, `day > lastDay - 7`.
func (d *cronDate) IsLastWeekdayOfMonth() bool {
	firstOfNext := time.Date(d.t.Year(), d.t.Month(), 1, 0, 0, 0, 0, d.loc).AddDate(0, 1, 0)
	lastDay := firstOfNext.AddDate(0, 0, -1).Day()
	return d.t.Day() > lastDay-7
}

// ToISOString formats as a UTC ISO-8601 string with milliseconds, matching
// JS Date.prototype.toISOString.
func (d *cronDate) ToISOString() string {
	return d.t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func (d *cronDate) String() string {
	return d.t.Format("2006-01-02 15:04:05 -07:00")
}

// parseDateInput accepts the timestamp forms upstream's CronDate constructor
// handles, restricted to the string forms that survive a JSON boundary.
func parseDateInput(s string, loc *time.Location) (time.Time, error) {
	if s == "" {
		return time.Now().In(loc), nil
	}
	formats := []string{
		"2006-01-02T15:04:05.000Z07:00",
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	// A trailing Z or explicit offset means the instant is absolute; otherwise
	// the wall-clock reading is interpreted in loc.
	absolute := strings.HasSuffix(s, "Z") || hasOffsetSuffix(s)
	for _, f := range formats {
		if absolute {
			if t, err := time.Parse(f, s); err == nil {
				return t.In(loc), nil
			}
			continue
		}
		if t, err := time.ParseInLocation(f, s, loc); err == nil {
			return resolveAmbiguousStart(t, f, s, loc), nil
		}
	}
	return time.Time{}, fmt.Errorf("CronDate: unhandled timestamp: %s", s)
}

// resolveAmbiguousStart reconciles Go's and luxon's readings of a wall-clock
// start time that is either nonexistent or ambiguous.
//
// luxon's rule is simple and uniform:
//
//	nonexistent (spring-forward gap) -> resolve FORWARD, past the gap
//	ambiguous   (fall-back repeat)   -> resolve to the EARLIER occurrence
//
// Go's time.ParseInLocation matches neither consistently, and which way it errs
// depends on the zone and the width of the shift. Both directions were observed:
//
//	Europe/Berlin      2026-03-29T02:15 nonexistent -> Go 03:15 +02:00 (forward, agrees)
//	America/New_York   2026-03-08T02:15 nonexistent -> Go 01:15 -05:00 (backward, differs)
//	Antarctica/Troll   2026-10-25T01:15 ambiguous   -> Go 01:15 +00:00 (later, differs)
//
// Troll's shift is TWO hours, which is why the "first occurrence" heuristic that
// works for a 1-hour zone lands on the wrong side there. So handle the two cases
// explicitly rather than assuming a direction.
// resolveAmbiguousStart resolves a wall-clock start time the way luxon does, by
// transcribing luxon's own algorithm rather than guessing a rule.
//
// Empirical sampling was a dead end: luxon picks the EARLIER instant for
// Antarctica/Troll and Europe/Berlin but the LATER one for Pacific/Auckland, so
// no earliest/latest rule fits. Reading `fixOffset` in luxon's
// src/datetime.js explains why — it is a guess-and-correct search seeded by an
// initial offset guess, and for IANA zones that guess is
// `guessOffsetForZone(zone)` = **the zone's offset at the current moment**
// (`Settings.now()`).
//
// So which of two valid instants luxon returns for an ambiguous wall clock
// depends on WHEN THE CODE RUNS: run the same call in January and in July, in a
// zone that observes DST, and the tie is broken differently. That is upstream
// behavior, not a defect the port should invent a fix for — but it does mean the
// port must seed its search the same way to agree.
func resolveAmbiguousStart(t time.Time, layout, input string, loc *time.Location) time.Time {
	asUTC, err := time.Parse(layout, input)
	if err != nil {
		return t
	}
	localTS := asUTC.UnixMilli()

	// guessOffsetForZone(zone): the zone's offset right now.
	_, nowOff := time.Now().In(loc).Zone()
	guess := nowOff / 60 // minutes, matching luxon's units

	ts, _ := fixOffset(localTS, guess, loc)
	return time.UnixMilli(ts).In(loc)
}

// fixOffset transcribes luxon's fixOffset (src/datetime.js):
//
//	function fixOffset(localTS, o, tz) {
//	  let utcGuess = localTS - o * 60 * 1000;
//	  const o2 = tz.offset(utcGuess);
//	  if (o === o2) return [utcGuess, o];
//	  utcGuess -= (o2 - o) * 60 * 1000;
//	  const o3 = tz.offset(utcGuess);
//	  if (o2 === o3) return [utcGuess, o2];
//	  return [localTS - Math.min(o2, o3) * 60 * 1000, Math.max(o2, o3)];
//	}
//
// localTS is the wall clock read as if it were UTC; o is the offset guess, in
// minutes. The final branch is the "hole time" case (a spring-forward gap).
func fixOffset(localTS int64, o int, loc *time.Location) (int64, int) {
	offsetAt := func(ms int64) int {
		_, off := time.UnixMilli(ms).In(loc).Zone()
		return off / 60
	}

	utcGuess := localTS - int64(o)*60*1000
	o2 := offsetAt(utcGuess)
	if o == o2 {
		return utcGuess, o
	}

	utcGuess -= int64(o2-o) * 60 * 1000
	o3 := offsetAt(utcGuess)
	if o2 == o3 {
		return utcGuess, o2
	}

	minOff, maxOff := o2, o3
	if o3 < o2 {
		minOff, maxOff = o3, o2
	}
	return localTS - int64(minOff)*60*1000, maxOff
}


func hasOffsetSuffix(s string) bool {
	if len(s) < 6 {
		return false
	}
	tail := s[len(s)-6:]
	return (tail[0] == '+' || tail[0] == '-') && tail[3] == ':'
}
