package cronparser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// predefined mirrors upstream's PredefinedExpressions. Note these are all
// SIX-field (seconds first).
var predefined = map[string]string{
	"@yearly":    "0 0 0 1 1 *",
	"@annually":  "0 0 0 1 1 *",
	"@monthly":   "0 0 0 1 * *",
	"@weekly":    "0 0 0 * * 0",
	"@daily":     "0 0 0 * * *",
	"@hourly":    "0 0 * * * *",
	"@minutely":  "0 * * * * *",
	"@secondly":  "* * * * * *",
	"@weekdays":  "0 0 0 * * 1-5",
	"@weekends":  "0 0 0 * * 0,6",
}

var monthAliases = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

var dowAliases = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

// validChars is per-field, because CronDayOfMonth and CronDayOfWeek OVERRIDE
// the base CronField.validChars getter to admit their special characters:
//
//	CronField        [?,*\dH/-]      (base)
//	CronDayOfMonth   [?,*\dLH/-]     adds L
//	CronDayOfWeek    [?,*\dLH#/-]    adds L and #
//
// Missing these overrides makes every "L" expression fail to parse — which is
// how it was caught: 44/440 generated cases errored with
// "Invalid characters, got value: L" while TypeScript accepted them.
var (
	validCharsBase = regexp.MustCompile(
		`^[?,*\dH/-]+$|^.*H\(\d+-\d+\)/\d+.*$|^.*H\(\d+-\d+\).*$|^.*H/\d+.*$`)
	validCharsDayOfMonth = regexp.MustCompile(
		`^[?,*\dLH/-]+$|^.*H\(\d+-\d+\)/\d+.*$|^.*H\(\d+-\d+\).*$|^.*H/\d+.*$`)
	validCharsDayOfWeek = regexp.MustCompile(
		`^[?,*\dLH#/-]+$|^.*H\(\d+-\d+\)/\d+.*$|^.*H\(\d+-\d+\).*$|^.*H/\d+.*$`)
)

func validCharsFor(unit Unit) *regexp.Regexp {
	switch unit {
	case UnitDayOfMonth:
		return validCharsDayOfMonth
	case UnitDayOfWeek:
		return validCharsDayOfWeek
	default:
		return validCharsBase
	}
}

var aliasRe = regexp.MustCompile(`(?i)[a-z]{3}`)

var hashRe = regexp.MustCompile(`H(?:\((\d+)-(\d+)\))?(?:/(\d+))?`)

var whitespaceRe = regexp.MustCompile(`\s+`)

// Options configures parsing and iteration.
type Options struct {
	CurrentDate string // RFC3339 or "2006-01-02T15:04:05"; empty means now
	StartDate   string
	EndDate     string
	TZ          string // IANA zone name; empty means UTC
	Strict      bool
	HashSeed    string
}

// Parse parses a cron expression.
//
// Upstream: CronExpressionParser.parse.
func Parse(expression string, opts Options) (*Expression, error) {
	rand := seededRandom(opts.HashSeed)

	if p, ok := predefined[expression]; ok {
		expression = p
	}

	raw, err := getRawFields(expression, opts.Strict)
	if err != nil {
		return nil, err
	}

	if opts.Strict && !(raw.dayOfMonth == "*" || raw.dayOfWeek == "*") {
		return nil, fmt.Errorf("Cannot use both dayOfMonth and dayOfWeek together in strict mode!")
	}

	second, secChars, err := parseField(UnitSecond, raw.second, rand)
	if err != nil {
		return nil, err
	}
	minute, minChars, err := parseField(UnitMinute, raw.minute, rand)
	if err != nil {
		return nil, err
	}
	hour, hourChars, err := parseField(UnitHour, raw.hour, rand)
	if err != nil {
		return nil, err
	}
	month, monthChars, err := parseField(UnitMonth, raw.month, rand)
	if err != nil {
		return nil, err
	}
	dom, domChars, err := parseField(UnitDayOfMonth, raw.dayOfMonth, rand)
	if err != nil {
		return nil, err
	}

	dowText, nth, err := parseNthDay(raw.dayOfWeek)
	if err != nil {
		return nil, err
	}
	dow, dowChars, err := parseField(UnitDayOfWeek, dowText, rand)
	if err != nil {
		return nil, err
	}

	fSecond, err := newField(UnitSecond, second, secChars, raw.second, 0)
	if err != nil {
		return nil, err
	}
	fMinute, err := newField(UnitMinute, minute, minChars, raw.minute, 0)
	if err != nil {
		return nil, err
	}
	fHour, err := newField(UnitHour, hour, hourChars, raw.hour, 0)
	if err != nil {
		return nil, err
	}
	fMonth, err := newField(UnitMonth, month, monthChars, raw.month, 0)
	if err != nil {
		return nil, err
	}
	// Upstream: CronDayOfMonth.fromMonth. When exactly ONE month is selected,
	// day values beyond that month's length are dropped — unless that would
	// empty the field, in which case the originals are kept.
	dom = filterDaysForSingleMonth(month, dom)

	fDom, err := newField(UnitDayOfMonth, dom, domChars, raw.dayOfMonth, 0)
	if err != nil {
		return nil, err
	}
	fDow, err := newField(UnitDayOfWeek, dow, dowChars, raw.dayOfWeek, nth)
	if err != nil {
		return nil, err
	}

	// Upstream: CronDayOfWeek's constructor rejects a STANDALONE "L" value —
	// only the suffix form ("5L" = last Friday) is legal for dayOfWeek. So
	// "0 0 0 * * L" and "0 0 0 * * 1,L" throw, while "0 0 0 * * 5L" does not.
	//
	// Found by the conformance replay: these two call sites in
	// CronExpressionParser.test.ts expected a throw and the port was returning
	// results.
	for _, c := range fDow.chars {
		if c == "L" {
			return nil, fmt.Errorf("CronDayOfWeek Validation error, unexpected standalone L")
		}
	}

	fields := &FieldCollection{
		Second:     fSecond,
		Minute:     fMinute,
		Hour:       fHour,
		DayOfMonth: fDom,
		Month:      fMonth,
		DayOfWeek:  fDow,
	}

	// Upstream: CronFieldCollection constructor. An explicit day of month that
	// can never occur is rejected — but ONLY when exactly one month is selected,
	// the field has no "L", and dayOfWeek is a wildcard (a restricted dayOfWeek
	// rescues the expression via the OR rule).
	//
	// Note upstream inspects values[0] ONLY, i.e. the smallest value after
	// sorting. So "31 11 *" throws but "1,31 11 *" does not, because 1 is
	// checked and 31 is never looked at. Preserved as-is.
	if len(fMonth.values) == 1 && !fDom.hasLastChar && fDow.IsWildcard() && len(fDom.values) > 0 {
		if fDom.values[0] > daysInMonth[fMonth.values[0]-1] {
			return nil, fmt.Errorf("Invalid explicit day of month definition")
		}
	}

	return newExpression(fields, expression, opts)
}

type rawFields struct {
	second     string
	minute     string
	hour       string
	dayOfMonth string
	month      string
	dayOfWeek  string
}

// getRawFields splits the expression into six fields, left-padding with
// defaults when fewer than six are supplied.
//
// Upstream: CronExpressionParser.#getRawFields.
func getRawFields(expression string, strict bool) (rawFields, error) {
	var rf rawFields

	if strict && len(expression) == 0 {
		return rf, fmt.Errorf("Invalid cron expression")
	}
	if expression == "" {
		expression = "0 * * * * *"
	}

	atoms := whitespaceRe.Split(strings.TrimSpace(expression), -1)
	if strict && len(atoms) < 6 {
		return rf, fmt.Errorf("Invalid cron expression, expected 6 fields")
	}
	if len(atoms) > 6 {
		return rf, fmt.Errorf("Invalid cron expression, too many fields")
	}

	// Upstream: atoms.unshift(...defaults.slice(atoms.length))
	//
	// Note this slices from atoms.length to the END, then prepends. For a
	// 5-field expression that is defaults.slice(5) === ["0"], so the SECONDS
	// field defaults to "0" — not "*". Getting this backwards makes every
	// 5-field expression fire once per second.
	defaults := []string{"*", "*", "*", "*", "*", "0"}
	if len(atoms) < len(defaults) {
		atoms = append(append([]string(nil), defaults[len(atoms):]...), atoms...)
	}

	rf.second = atoms[0]
	rf.minute = atoms[1]
	rf.hour = atoms[2]
	rf.dayOfMonth = atoms[3]
	rf.month = atoms[4]
	rf.dayOfWeek = atoms[5]
	return rf, nil
}

// parseField resolves aliases, expands wildcards and H, then parses the
// comma-separated sequence. Returns numeric values and any special-char values.
//
// Upstream: CronExpressionParser.#parseField.
func parseField(unit Unit, value string, rand prng) ([]int, []string, error) {
	c := unitConstraints[unit]

	if unit == UnitMonth || unit == UnitDayOfWeek {
		var aliasErr error
		value = aliasRe.ReplaceAllStringFunc(value, func(m string) string {
			lower := strings.ToLower(m)
			if v, ok := monthAliases[lower]; ok && unit == UnitMonth {
				return strconv.Itoa(v)
			}
			if v, ok := dowAliases[lower]; ok {
				return strconv.Itoa(v)
			}
			if v, ok := monthAliases[lower]; ok {
				return strconv.Itoa(v)
			}
			aliasErr = fmt.Errorf("Validation error, cannot resolve alias \"%s\"", lower)
			return m
		})
		if aliasErr != nil {
			return nil, nil, aliasErr
		}
	}

	if !validCharsFor(unit).MatchString(value) {
		return nil, nil, fmt.Errorf("Invalid characters, got value: %s", value)
	}

	value = parseWildcard(value, c)
	value, err := parseHashed(value, c, rand)
	if err != nil {
		return nil, nil, err
	}
	return parseSequence(unit, value, c)
}

// parseWildcard replaces * and ? with the field's full range.
//
// Upstream: CronExpressionParser.#parseWildcard.
func parseWildcard(value string, c constraints) string {
	repl := fmt.Sprintf("%d-%d", c.min, c.max)
	value = strings.ReplaceAll(value, "*", repl)
	return strings.ReplaceAll(value, "?", repl)
}

// parseHashed expands Jenkins-style H values using the seeded PRNG.
//
// Upstream: CronExpressionParser.#parseHashed. The PRNG is drawn ONCE before
// the replace loop, so every H in a single field shares the same random value.
func parseHashed(value string, c constraints, rand prng) (string, error) {
	randomValue := rand()

	var hashErr error
	out := hashRe.ReplaceAllStringFunc(value, func(match string) string {
		sub := hashRe.FindStringSubmatch(match)
		minS, maxS, stepS := sub[1], sub[2], sub[3]

		switch {
		case minS != "" && maxS != "" && stepS != "":
			minNum, _ := strconv.Atoi(minS)
			maxNum, _ := strconv.Atoi(maxS)
			stepNum, _ := strconv.Atoi(stepS)
			if minNum > maxNum {
				hashErr = fmt.Errorf("Invalid range: %d-%d, min > max", minNum, maxNum)
				return match
			}
			if stepNum <= 0 {
				hashErr = fmt.Errorf("Invalid step: %d, must be positive", stepNum)
				return match
			}
			minStart := minNum
			if c.min > minStart {
				minStart = c.min
			}
			offset := int(randomValue * float64(stepNum))
			var vals []string
			for i := (minStart/stepNum)*stepNum + offset; i <= maxNum; i += stepNum {
				if i >= minStart {
					vals = append(vals, strconv.Itoa(i))
				}
			}
			return strings.Join(vals, ",")

		case minS != "" && maxS != "":
			minNum, _ := strconv.Atoi(minS)
			maxNum, _ := strconv.Atoi(maxS)
			if minNum > maxNum {
				hashErr = fmt.Errorf("Invalid range: %d-%d, min > max", minNum, maxNum)
				return match
			}
			return strconv.Itoa(int(randomValue*float64(maxNum-minNum+1)) + minNum)

		case stepS != "":
			stepNum, _ := strconv.Atoi(stepS)
			if stepNum <= 0 {
				hashErr = fmt.Errorf("Invalid step: %d, must be positive", stepNum)
				return match
			}
			offset := int(randomValue * float64(stepNum))
			var vals []string
			for i := (c.min/stepNum)*stepNum + offset; i <= c.max; i += stepNum {
				if i >= c.min {
					vals = append(vals, strconv.Itoa(i))
				}
			}
			return strings.Join(vals, ",")

		default:
			return strconv.Itoa(int(randomValue*float64(c.max-c.min+1)) + c.min)
		}
	})

	return out, hashErr
}

// parseSequence parses a comma-separated list of atoms.
//
// Upstream: CronExpressionParser.#parseSequence.
func parseSequence(unit Unit, val string, c constraints) ([]int, []string, error) {
	var nums []int
	var chars []string

	atoms := strings.Split(val, ",")
	for _, atom := range atoms {
		if len(atom) == 0 {
			return nil, nil, fmt.Errorf("Invalid list value format")
		}
		rangeNums, rangeChars, err := parseRepeat(unit, atom, c)
		if err != nil {
			return nil, nil, err
		}
		nums = append(nums, rangeNums...)
		chars = append(chars, rangeChars...)
	}
	return nums, chars, nil
}

// parseRepeat handles the "range/step" form.
//
// Upstream: CronExpressionParser.#parseRepeat. When a step is present but the
// left side has no "-", upstream rewrites it as "start-max" — so "5/10" means
// "5-max/10", NOT "just 5 every 10".
func parseRepeat(unit Unit, val string, c constraints) ([]int, []string, error) {
	atoms := strings.Split(val, "/")
	if len(atoms) > 2 {
		return nil, nil, fmt.Errorf("Invalid repeat: %s", val)
	}
	if len(atoms) == 2 {
		left := atoms[0]
		if !strings.Contains(left, "-") {
			left = fmt.Sprintf("%s-%d", left, c.max)
		}
		step, err := strconv.Atoi(atoms[1])
		if err != nil {
			return nil, nil, fmt.Errorf("Constraint error, cannot repeat at every %s time.", atoms[1])
		}
		return parseRange(unit, left, step, c)
	}
	return parseRange(unit, val, 1, c)
}

// parseRange parses "min-max" or a bare value.
//
// Upstream: CronExpressionParser.#parseRange.
func parseRange(unit Unit, val string, step int, c constraints) ([]int, []string, error) {
	atoms := strings.Split(val, "-")
	if len(atoms) <= 1 {
		// Bare value: a number, or a special char such as "L".
		if n, err := strconv.Atoi(val); err == nil {
			if n < c.min || n > c.max {
				return nil, nil, fmt.Errorf("Constraint error, got value %d expected range %d-%d", n, c.min, c.max)
			}
			if unit == UnitDayOfWeek {
				n = n % 7
			}
			return []int{n}, nil, nil
		}
		if isValidConstraintChar(c, val) {
			return nil, []string{val}, nil
		}
		return nil, nil, fmt.Errorf("Constraint error, got value %s expected range %d-%d", val, c.min, c.max)
	}

	// JS: `atoms.map((num) => parseInt(num, 10))`, then #validateRange formats
	// the values back into the message with template interpolation. parseInt("")
	// is NaN, and String(NaN) is "NaN" — so "-1 * * * * *" splits into ["", "1"]
	// and reports "got range NaN-1", not "got range -1".
	//
	// Go's Atoi returns an error instead, so the NaN presentation has to be
	// reproduced explicitly. Caught by the conformance replay against the
	// upstream suite's own expected messages.
	minNum, errMin := strconv.Atoi(atoms[0])
	maxNum, errMax := strconv.Atoi(atoms[1])
	if errMin != nil || errMax != nil || minNum < c.min || maxNum > c.max {
		return nil, nil, fmt.Errorf("Constraint error, got range %s-%s expected range %d-%d",
			jsNumberText(atoms[0]), jsNumberText(atoms[1]), c.min, c.max)
	}
	if minNum > maxNum {
		return nil, nil, fmt.Errorf("Invalid range: %d-%d, min(%d) > max(%d)", minNum, maxNum, minNum, maxNum)
	}
	if step <= 0 {
		return nil, nil, fmt.Errorf("Constraint error, cannot repeat at every %d time.", step)
	}

	return createRange(unit, minNum, maxNum, step), nil, nil
}

// createRange expands min..max by step.
//
// Upstream: CronExpressionParser.#createRange. The dayOfWeek special case
// pre-pushes 0 when max is a multiple of 7, so "0-7" yields Sunday once at the
// front rather than 7 mapping to a duplicate 0.
func createRange(unit Unit, min, max, step int) []int {
	var stack []int
	if unit == UnitDayOfWeek && max%7 == 0 {
		stack = append(stack, 0)
	}
	for i := min; i <= max; i += step {
		found := false
		for _, v := range stack {
			if v == i {
				found = true
				break
			}
		}
		if !found {
			stack = append(stack, i)
		}
	}
	return stack
}

// parseNthDay splits the "#n" occurrence suffix off a dayOfWeek field.
//
// Upstream: CronExpressionParser.#parseNthDay.
func parseNthDay(val string) (string, int, error) {
	atoms := strings.Split(val, "#")
	if len(atoms) <= 1 {
		return atoms[0], 0, nil
	}

	if m := regexp.MustCompile(`([,\-/])`).FindString(val); m != "" {
		return "", 0, fmt.Errorf(
			"Constraint error, invalid dayOfWeek `#` and `%s` special characters are incompatible", m)
	}

	nth, err := strconv.Atoi(atoms[len(atoms)-1])
	if err != nil || len(atoms) > 2 || nth < 1 || nth > 5 {
		return "", 0, fmt.Errorf("Constraint error, invalid dayOfWeek occurrence number (#)")
	}
	return atoms[0], nth, nil
}

func isValidConstraintChar(c constraints, value string) bool {
	for _, ch := range c.chars {
		if strings.Contains(value, ch) {
			return true
		}
	}
	return false
}

// daysInMonth mirrors CronMonth.daysInMonth. Note February is 29 here:
// upstream's table is leap-permissive, so "29 * 2" survives parsing and the
// leap-year check happens during iteration instead.
var daysInMonth = []int{31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}

// filterDaysForSingleMonth implements CronDayOfMonth.fromMonth.
func filterDaysForSingleMonth(months, days []int) []int {
	if len(months) != 1 {
		return days
	}
	limit := daysInMonth[months[0]-1]
	var kept []int
	for _, d := range days {
		if d <= limit {
			kept = append(kept, d)
		}
	}
	if len(kept) > 0 {
		return kept
	}
	return days
}

// jsNumberText renders a field atom the way JS String(parseInt(s, 10)) would:
// an unparseable atom becomes "NaN".
//
// Upstream's error messages interpolate the PARSED values, so a malformed range
// like "-1" (which splits to ["", "1"]) reports "NaN-1". Reproducing the text
// exactly matters because the upstream suite asserts on these strings.
func jsNumberText(s string) string {
	if n, err := strconv.Atoi(s); err == nil {
		return strconv.Itoa(n)
	}
	return "NaN"
}
