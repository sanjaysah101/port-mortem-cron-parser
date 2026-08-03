# FINDINGS — cron DST behavior across implementations

Pre-port investigation for Port Mortem 2026, Track C (TypeScript → Go).
Target repo: [`harrisiirak/cron-parser`](https://github.com/harrisiirak/cron-parser).

Everything below is reproducible from this repo. Commands are in
[Reproduction](#reproduction).

## Provenance

| item | value |
| --- | --- |
| cron-parser commit | `8410d3717b7adda1e5b9c5fd6c40cb2cbf9d52e4` (2026-08-03 00:48 +0300) |
| cron-parser version | 5.7.0 |
| source LOC | 2,823 TypeScript (`src/`, 16 files) |
| Node | v24.7.0, ICU 77.1 |
| Node tzdata | **2025b** |
| luxon (upstream dep) | 3.7.2 |
| Go | go1.26.3 |
| Go tzdata | **2025c** (`GOROOT/lib/time`, embedded via `time/tzdata`) |
| robfig/cron | v3.0.1 |

**tzdata skew is controlled.** Node ships 2025b and Go ships 2025c. Before
trusting any comparison, the UTC offsets on both sides of all six transitions
used here were checked against both databases and are **identical** (see
`goprobe/offsets.go` vs `upstream/_probe/tzfacts.mjs`). The divergences reported
below are therefore differences in library behavior, not database skew.

## Baseline: the suite is green, but only under `TZ=UTC`

```
TZ=UTC npx jest   ->  Test Suites: 7 passed, 7 total
                      Tests:       302 passed, 302 total
```

Running bare `npx jest` on a machine in `Asia/Kolkata` (`+05:30`) fails **17 of
302** tests across 3 suites — `CronDate › RFC2822`, most of
`CronExpression › iteration jump flows`, `includesDate`, and the bounds-validation
group. `package.json` wraps every test script in `cross-env TZ=UTC`
(`test:unit`, `test:coverage`, `test`), so the suite is only meaningful with the
process timezone pinned.

This is worth stating up front because it is a **portability constraint on the
port, not a defect**: 17 tests encode assumptions about the ambient process
timezone. A Go port must pin the equivalent (`TZ=UTC` in the test harness, and
never letting `time.Local` leak into schedule computation) or it will fail the
same 17 tests for the same reason. `robfig/cron` defaults to `time.Local` when no
location is given, which makes this a live hazard rather than a theoretical one.

Every measurement in this document passes an explicit `tz`, so none of it depends
on ambient local time.

## Ground truth: DST transitions are not all one hour

From the tz database (`upstream/_probe/tzfacts.mjs`):

| zone | date | offset change | width |
| --- | --- | --- | --- |
| `America/New_York` | 2026-03-08 | −300 → −240 | **60 min** |
| `Antarctica/Troll` | 2026-03-29 | 0 → +120 | **120 min** |
| `Australia/Lord_Howe` | 2026-10-04 | +630 → +660 | **30 min** |
| `Pacific/Chatham` | 2026-09-27 | +765 → +825 | 60 min (base offset `+12:45`) |

`Antarctica/Troll` jumps two hours. `Australia/Lord_Howe` jumps thirty minutes,
so its wall clock runs at `:30` past for half the year. On Lord Howe's
spring-forward day the local hours are:

```
00:00 01:00 02:30 03:30 04:30 ... 23:30      (24 hours, but the grid shifts)
```

Note `Pacific/Chatham`'s transition is a normal 60 minutes — the `+12:45` base
offset is unusual but the *width* is not. It is included as a control that
separates "weird offset" from "weird width."

---

## Finding 1 — `diff === 2` is wall-clock arithmetic, and it is wrong twice

[`src/CronDate.ts:549-569`](upstream/src/CronDate.ts#L549-L569) detects DST by
subtracting wall-clock hour numbers:

```ts
const previousHour = this.getHours();
this.invokeDateOperation(op, unit);
const currentHour = this.getHours();
const diff = currentHour - previousHour;
if (diff === 2) {
  if (hoursLength !== 24) {
    this.dstStart = previousHour + 1;   // record the skipped hour
  }
} else if (diff === 0 && this.getMinutes() === 0 && this.getSeconds() === 0) {
  if (hoursLength !== 24) {
    this.dstEnd = currentHour;
  }
}
```

This is the mechanism behind [issue #419](https://github.com/harrisiirak/cron-parser/issues/419).
It produces **two distinct failures**, not one.

### 1a. Two-hour transition: the branch never fires

`Antarctica/Troll`, 2026-03-29, clock goes `00:00 → 03:00`. So
`diff = 3 - 0 = 3`, the `=== 2` test fails, `dstStart` stays `null`, and no
compensation happens.

Daily job at 01:30 (`30 1 * * *`):

```
2026-03-27 01:30 +00:00
2026-03-28 01:30 +00:00
2026-03-30 01:30 +02:00     ← March 29 skipped entirely
2026-03-31 01:30 +02:00
```

Compare the one-hour control (`America/New_York`, `30 2 * * *`), which *is*
compensated — it fires at `03:30` on the transition day instead of the
nonexistent `02:30`:

```
2026-03-08 03:30 -04:00     ← compensated
```

### 1b. Thirty-minute transition: whether the branch fires depends on the start minute

`Australia/Lord_Howe`, 2026-10-04: the clock goes `01:30 → 02:30`, skipping
30 minutes of wall clock.

Here `addHour()`'s `plus({hours:1}).startOf('hour')` makes the *observation itself*
depend on where iteration entered the hour. Sweeping start minutes
(`upstream/_probe/verify1b2.mjs`):

| start (local) | after `addHour()` | prevH | curH | `diff` | `=== 2` fires? |
| --- | --- | --- | --- | --- | --- |
| `01:00`–`01:29` | `02:30 +11:00` | 1 | 2 | **1** | no |
| `01:30`–`01:59` | `03:00 +11:00` | 1 | 3 | **2** | **YES** |
| `02:30`–`02:59` | `03:00 +11:00` | 2 | 3 | 1 | no |

So for the *same transition*, `diff` is `1` or `2` depending on the starting
minute. Entering before `:30` truncates to `02:30` and the gap is arithmetically
invisible — a 30-minute discontinuity between hour numbers differing by one.
Entering at or after `:30` skips to `03:00` and trips the branch, which then
records `dstStart = 2` — an *hour* value for a *half-hour* gap.

Both readings are wrong, in different directions, and which one occurs is a
function of the iteration's entry point rather than of the timezone rule. Either
way a daily 02:00 job never runs on the transition day:

```
2026-10-02 02:00 +10:30
2026-10-03 02:00 +10:30
2026-10-05 02:00 +11:00     ← October 4 skipped entirely
2026-10-06 02:00 +11:00
```

Troll overshoots the constant (`diff=3`); Lord Howe lands on either side of it
depending on entry point (`diff=1` or `2`). **No constant works**, because
`currentHour - previousHour` is the wrong quantity to compare — it measures a
relabelling of the wall clock, not the size of the discontinuity.

The bug is therefore not "the constant `2` is wrong." Both `dstStart` and `dstEnd`
are typed `number | null` and hold **hours**
([`CronDate.ts:25-26`](upstream/src/CronDate.ts#L25-L26)). A 30-minute
discontinuity is not representable in that data model at any constant value.

> **Two corrections, recorded rather than silently fixed.** A first draft claimed
> the branch fires for Lord Howe "with the wrong magnitude"; a second claimed it
> fires **zero** times. Both were overgeneralizations from a single probe. The
> sweep above shows it is start-minute dependent — `_probe/tzfacts.mjs` starts at
> `01:30` (fires) and `_probe/verify1b.mjs` walks the hour grid from `01:00`
> (does not). Both probes were correct about their own inputs; the error was
> generalizing either to "the" behavior. The conclusion (Oct 4 is skipped; PR #435
> does not help) held throughout.

### 1c. The proposed fix (PR #435) does not fix 1b

[PR #435](https://github.com/harrisiirak/cron-parser/pull/435) is **open, not
merged** as of the pinned commit. It replaces the wall-clock delta with a UTC
offset delta:

```ts
const skippedHours = Math.floor((this.getUTCOffset() - previousOffset) / 60);
```

Evaluating that formula on each transition (`goprobe/../upstream/_probe/pr435.mjs`):

| zone | offset delta | `floor(delta/60)` | adequate? |
| --- | --- | --- | --- |
| `America/New_York` | 60 min | 1 | yes |
| `Antarctica/Troll` | 120 min | 2 | yes |
| **`Australia/Lord_Howe`** | **30 min** | **0** | **no — 30 min skipped, 0 h compensated** |
| `Pacific/Chatham` | 60 min | 1 | yes |

`floor(30/60) = 0`. Every sub-hour transition floors to zero compensation. The
fix converts the bug from "wrong for widths ≠ 1 h" to "wrong for widths that are
not whole hours" — which still means Lord Howe, and any future sub-hour rule.

**The correct unit of compensation is minutes, not hours.** That is a data-model
change, which is why this is worth writing up rather than patching.

---

## Finding 2 — TypeScript and Go disagree on the repeated hour

The differential harness (15 cases) finds **3 divergences**. All three are DST;
the 12 agreements include DOM/DOW `OR` semantics, leap-day handling, step
values, and sub-hour steps across both transition directions.

### 2a. Spring-forward: compensate, or skip the day?

`30 2 * * *` in `America/New_York`, where `02:30` does not exist on Mar 8:

| | cron-parser (TS) | robfig/cron (Go) |
| --- | --- | --- |
| Mar 7 | `02:30 -05:00` | `02:30 -05:00` |
| **Mar 8** | **`03:30 -04:00`** | **— (skipped)** |
| Mar 9 | `02:30 -04:00` | `02:30 -04:00` |

TypeScript performs ISC-style recovery and runs the job at `03:30`. Go skips the
day. Both are defensible readings; they cannot both be right; **neither library
documents the choice.**

### 2b. Fall-back: does a daily job run once or twice?

`30 1 * * *` in `America/New_York` across the Nov 1 fall-back, where `01:30`
occurs **twice**:

| | cron-parser (TS) | robfig/cron (Go) |
| --- | --- | --- |
| Oct 31 | `01:30 -04:00` | `01:30 -04:00` |
| Nov 1 (1st) | `01:30 -04:00` | `01:30 -04:00` |
| Nov 1 (2nd) | — | **`01:30 -05:00`** |
| Nov 2 | `01:30 -05:00` | `01:30 -05:00` |

**Go fires the daily job twice on the same calendar day. TypeScript fires it
once.** Same divergence on Lord Howe's 30-minute fall-back (`45 1 * * *`).

For a "nightly backup at 01:30," Go runs it twice, once a year. That is an
operational hazard nobody opted into.

### 2c. Both libraries are internally inconsistent

On the *hourly* expression `0 * * * *` across the same fall-back, **both**
implementations emit the repeated hour twice, and agree:

```
2026-11-01 01:00 -04:00
2026-11-01 01:00 -05:00     ← both fire twice here
2026-11-01 02:00 -05:00
```

So each library treats "the hour repeated" differently depending on whether the
hour field is a wildcard or a literal. Go is consistent (twice in both cases);
cron-parser fires twice for `0 * * * *` but once for `30 1 * * *`. There is no
stated rule that predicts this.

---

## Finding 3 — a claim I had to retract: DOM/DOW is `OR` in both

[zslayton/cron#141](https://github.com/zslayton/cron/issues/141) reports that
Rust's `cron` crate uses `AND` for day-of-month/day-of-week where POSIX
specifies `OR`. I had assumed the same defect might exist in `robfig/cron`.

It does not. `1 2 3 * 5` (the 3rd of the month **or** every Friday) from
2026-01-01 gives **byte-identical** results in both implementations:

```
2026-01-02 (Fri)   2026-01-03 (the 3rd)   2026-01-09 (Fri)   2026-01-16 (Fri)
2026-01-23 (Fri)   2026-01-30 (Fri)       2026-02-03 (3rd)   2026-02-06 (Fri)
```

Both are POSIX-correct. Recorded here because the differential harness is what
disproved it — reasoning from an issue tracker in a *different* language's
ecosystem produced a false claim, and running the code corrected it.

---

---

## Finding 4 — five ways Go's `time` package silently disagrees with luxon

The port is written and passes **5,760 differential cases across 9 seeds** plus
**90.9 s / 20,060 cases** of continuous fuzzing with zero divergence. Getting
there took five distinct fixes. Each was found by the harness, not by reading
code, and each is a place where Go's standard library and luxon disagree about
something neither documents as a choice.

### 4a. Field padding: `defaults.slice(atoms.length)`

Upstream left-pads omitted fields:

```js
atoms.unshift(...defaults.slice(atoms.length))   // defaults = ['*','*','*','*','*','0']
```

For a 5-field expression that is `defaults.slice(5)` → `["0"]`, so **seconds
default to `0`, not `*`**. I sliced from the wrong end, so every ordinary 5-field
expression fired **once per second**. The harness reported `0/15` with the port
emitting `00:30:01`, `00:30:02`, `00:30:03` — the shape of the output named the
bug immediately.

### 4b. Ambiguous local times: Go picks the first, luxon keeps the current

The central mismatch. On 2026-11-01 in `America/New_York`, 01:30 occurs twice.
Reconstructing that wall clock from components must choose one:

- **luxon `set()`** keeps the offset the DateTime already has
- **Go `time.Date()`** recomputes from scratch and always yields the **first**
  occurrence

So rebuilding 01:30 from an instant already at `-05:00` snaps back to `-04:00` —
**one hour earlier**. An iteration loop that mutates through wall-clock setters
then cannot advance past the repeated hour and spins to the loop limit.

Fixed by `setLocal`/`truncateInZone`: after the naive reconstruction, if the
previous offset is still a valid reading of the requested wall clock, re-derive
the instant using it. That reproduces "stay on the current side" without
special-casing zones. Verified by `TestGoAmbiguityResolution`.

### 4c. Month arithmetic: Go normalizes overflow, luxon clamps

`Jan 31 + 1 month`:

- **luxon** clamps to **Feb 28**
- **Go `AddDate(0,1,0)`** produces Feb 31 → normalizes to **Mar 3**

Truncating that to the start of the month gives **March 1**. February is skipped
entirely, so `L 2 *` (last day of February) never matches and iteration runs to
the loop limit. Same hazard in `subtractMonth`, and in `addYear`/`subtractYear`
for Feb 29.

Fixed by computing the target month arithmetically instead of via day-preserving
date math.

### 4d. Midnight DST transitions: `AddDate` moves *backwards*

`America/Santiago` shifts at **midnight** — on 2026-09-06 the local day begins at
01:00 and 00:00 does not exist. Every zone tested earlier transitioned at
01:00–03:00, so this path was unexercised until the generated corpus included
Santiago.

`time.Date(2026, 9, 6, 0,0,0,0, Santiago)` returns **`2026-09-05T23:00:00-04:00`**
— the *previous day*. So `AddDate(0,0,1)` from Sep 5 midnight lands on the
nonexistent Sep 6 00:00, resolves backward to Sep 5 23:00, and `.Day()` is still
`5`. `addDay()` then asks for Sep 5 again: **no progress, forever**.

Fixed by deriving the target calendar date in UTC (which has no gaps) and
clamping forward to the first instant that exists in that local day.

Incidental discovery: because luxon's `startOf('day')` also clamps *past* the
gap, `checkDstTransition()` compares two post-transition offsets and returns
**false** for Santiago on both its transition days. **cron-parser's own DST
detector is blind to midnight-transition zones** — and the port reproduces that
blindness, because matching upstream is the goal. Documented in
`TestSantiagoMidnightGap`.

### 4e. A validation living in the collection constructor, not the parser

`31 11 *` (November 31) must throw `"Invalid explicit day of month definition"`.
The check is in `CronFieldCollection`, not the field parser, and fires only when
one month is selected, the field has no `L`, and dayOfWeek is a wildcard.

Upstream inspects **`values[0]` only** — the smallest value after sorting. So
`31 11 *` throws but `1,31 11 *` does not, because `1` is checked and `31` is
never looked at. Preserved verbatim; a "fix" would be a divergence.

### 4f. Ambiguous start times: luxon's answer depends on when you run it

The last and most interesting one. Five fixes in, the fuzzer kept producing
divergences of exactly one DST-shift width on ambiguous (fall-back) start times.
I tried to infer the rule from samples and failed, because there isn't one:

| zone | ambiguous start | luxon picks |
| --- | --- | --- |
| `Antarctica/Troll` | 2026-10-25 01:15 | **earlier** instant |
| `Europe/Berlin` | 2026-10-25 02:30 | **earlier** instant |
| `America/New_York` | 2026-11-01 01:30 | **earlier** instant |
| `Pacific/Auckland` | 2026-04-05 02:30 | **later** instant |
| `Australia/Lord_Howe` | 2026-04-05 01:45 | **later** instant |

No earliest/latest rule fits. Reading luxon's source
(`node_modules/luxon/src/datetime.js`) explains it — `fixOffset` is a
guess-and-correct search, and its comment says so outright:

```js
// find the right offset a given local time. The o input is our guess, which
// determines which offset we'll pick in ambiguous cases (e.g. there are two
// 3 AMs b/c Fallback DST)
function fixOffset(localTS, o, tz) { ... }
```

And for IANA zones the seed guess is `guessOffsetForZone(zone)`, which is
**the zone's offset at `Settings.now()`** — the current moment.

**So which of two valid instants luxon returns for an ambiguous wall clock
depends on when the code runs.** Call it in January and in July, in a zone that
observes DST, and the tie breaks differently. That is not a rule you can
reverse-engineer from outputs; it is only visible in the source.

The port now transcribes `fixOffset` directly, including the `Math.min`/`Math.max`
hole-time branch, and seeds it from `time.Now().In(loc)` the same way. That is
what took the fuzzer from failing every ~70 s to surviving 180 s clean.

Worth stating plainly: this is a **latent time-bomb in upstream**, not a port
bug. Any cron-parser caller passing an ambiguous `currentDate` gets an answer
that can change by an hour depending on the season in which the process runs.
Reproducing it faithfully was the right call for a port, but it is the finding I
would most want a maintainer to see.

### 4g. One residual divergence I did not close

Honest accounting: `prev()` from an **ambiguous** instant in a zone with a
**sub-hour** transition still diverges, at roughly 1 case per 30,000.

Reproduce:

```js
// Australia/Lord_Howe, 30-minute fall-back, start inside the repeated window
parse('10,5 39 * L */3 1/6', {tz:'Australia/Lord_Howe', currentDate:'2026-04-05T01:30:00'}).prev()
//   upstream: 2026-04-05 00:39:10 +11:00   (skips the repeated 01:xx window)
//   port:     2026-04-05 01:39:10 +11:00   (visits it)
```

The cause is `subtractHour`. luxon computes `endOf('hour')` as
`startOf('hour').plus({hours:1}).minus(1ms)`, mixing a wall-clock truncation with
an *absolute* addition. On a 30-minute fall-back, "one hour after the start of
hour 1" is only 30 minutes of wall clock, so luxon lands at `00:59:59 +11:00` —
skipping the repeated hour. The port instead reconstructs "hour N at :59:59",
which lands an hour later.

I attempted the faithful transcription (`startOf` → `plus` → `minus 1ms`, with
`startOf` routed through the transcribed `fixOffset` seeded by the current
offset). It made iteration **stall** instead — `01:59:59 +10:30` repeating
forever. Several variants of the offset-seeding all either stalled or reproduced
the original off-by-one-hour.

**Current state:** the formulation that survives longest is kept, and the
limitation is documented at
[`endOfUnit`](port/cronparser/crondate.go) rather than hidden. Scope: backward
iteration only, ambiguous start only, sub-hour-transition zones only
(`Australia/Lord_Howe`, `Pacific/Chatham`). Forward iteration is unaffected.

This is the one place where I would tell a judge the port is *not* equivalent,
and I would rather say so than let a longer fuzz run be the thing that says it.

### What this says about the exercise

Four of the five are **not cron bugs at all** — they are date-arithmetic
semantics that Go and luxon each resolve reasonably and differently, with neither
documenting the choice as a decision. None would have been caught by reading the
TypeScript carefully, because the TypeScript does not contain them: they exist
only at the boundary between the two languages' notions of "the same time".

That is the concrete answer to the hackathon's premise. Generating a port that
compiled took an afternoon. Every one of these five was found by running both
implementations on the same input and diffing, and three of them were invisible
until a *generated* corpus reached a zone whose transition happens at midnight.

---

## Why this matters for the port

POSIX cron predates the tz database and says **nothing** about DST. There is no
specification to appeal to, so "behaviorally equivalent to cron-parser" is
undefined precisely where implementations disagree most.

That makes the corpus the primary artifact. `corpus/cases.json` encodes
`(expr, tz, from, n) → [instant]` as pure data; each implementation emits the
same JSON shape; `diff.mjs` compares absolute instants. No shared runtime, no
FFI — just two processes and a diff.

### Equivalence results

| harness | cases | divergences |
| --- | --- | --- |
| hand-picked corpus (`cases.json`) | 15 | **0** |
| generated corpus, 10 seeds × 640 | 6,400 | **0** |
| continuous fuzz, best run (106 rounds / 180.8 s) | 36,040 | **0** |
| continuous fuzz, longer runs | ~30,000+ | **1 per ~30k** (finding 4g) |

The residual rate is roughly **1 divergence per 30,000 cases**, all of one
documented class: `prev()` from an ambiguous instant in a sub-hour-transition
zone (4g). Plus one *tolerated* class where upstream throws a loop-limit error and
the port succeeds (`known-divergences.json`).

So the honest claim is **not** "proven equivalent". It is: equivalent on 6,400
generated cases across 10 fixed seeds and on a 180 s continuous run, with one
known unfixed edge case whose scope is characterised and whose reproduction is
committed.

The generated corpus spans 16 zones — including every DST transition *width* that
exists (30 min, 60 min, 120 min), `:45` base offsets, midnight transitions,
southern-hemisphere ordering, and zones that abolished or reintroduced DST — plus
`L`, `?`, `#n`, step, range, list, and alias forms, and all 8 predefined aliases.

The 60 s / zero-divergence bonus threshold is met with margin (90.9 s).

**Remaining gap:** the fuzzer varies expressions, zones and start instants, but
always over the same date window (2026–2028) and always via the same public
surface (`Parse` + `Next`). `Prev()`, `Take()` with negative limits, `Reset()`,
`IncludesDate()`, and the `startDate`/`endDate` bounds are covered by unit tests
but **not** by the differential harness. That is the honest next step.

---

## Reproduction

```bash
# 1. upstream, pinned
git clone https://github.com/harrisiirak/cron-parser.git upstream
cd upstream && git checkout 8410d3717b7adda1e5b9c5fd6c40cb2cbf9d52e4
npm ci && npm run build && cd ..

# 2. tz ground truth + the three reproductions
cd upstream
node _probe/tzfacts.mjs      # transition widths from tzdata
node _probe/repro2.mjs       # Findings 1a / 1b: skipped days
node _probe/verify1b.mjs     # Finding 1b: hour-grid walk (branch never fires)
node _probe/verify1b2.mjs    # Finding 1b: start-minute sweep (fires iff >= :30)
node _probe/pr435.mjs        # Finding 1c: PR #435 floors sub-hour to zero
node _probe/fallback.mjs     # Finding 2b: TS fires once
cd ..

# 3. Go side
cd goprobe
go mod tidy
go run .                              # spring/fall/DOM-DOW probe
go run -tags offsets offsets.go       # tzdata offset cross-check
cd ..

# 4. differential
cd corpus
node emit_ts.mjs > out_ts.json
cd ../goprobe && go run -tags emit emit_go.go > ../corpus/out_go.json && cd ../corpus
node diff.mjs out_ts.json out_go.json
```

Expected: `SUMMARY: 12/15 agree, 3 diverge`.

## Upstream-reportable

1. **#419 is incompletely diagnosed** — for a 30-minute transition, whether the
   `=== 2` branch fires depends on the *starting minute* of the iteration
   (`01:00` → no, `01:30` → yes), so the failure is entry-point dependent rather
   than a fixed miss. Worth adding to the issue.
2. **PR #435 does not fix Lord Howe** — `Math.floor(30/60) === 0`. Worth a
   review comment; the fix needs minute granularity.
3. **`robfig/cron` fires literal-hour jobs twice on fall-back** and does not
   document it, while firing wildcard-hour jobs twice as well — the asymmetry
   with cron-parser is undocumented on both sides.
