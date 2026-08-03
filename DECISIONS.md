# DECISIONS

Port Mortem 2026 · Track C (TypeScript → Go) · `harrisiirak/cron-parser` → Go

This log covers decisions made during **repo selection and pre-port
investigation**. The port itself is not yet written; see
[Status](#status-and-what-is-not-done) for exactly where the line is.

---

## D1 — Repo: `harrisiirak/cron-parser`

**Chosen** over bytes.js, mustache.js, picomatch, qs, TinyColor, mnemonist,
croniter, dateparse, go-diff, parsimonious, ruby-units, gobwas/glob.

**Why.** The selection criterion was not size or popularity but whether the port
would *surface* something. cron-parser has an open, reproducible correctness bug
([#419](https://github.com/harrisiirak/cron-parser/issues/419)) in a domain with
**no governing specification** — POSIX cron predates the tz database and says
nothing about DST. Where there is no spec, "behaviorally equivalent" is undefined
precisely where implementations disagree, which makes the equivalence question
real rather than mechanical.

**Rejected, and why** (each verified, not assumed):

| candidate | reason |
| --- | --- |
| `mustache.js` → Rust | **Ineligible.** `rust-mustache` exists, ~104k downloads/mo. |
| `life4/textdistance` → Rust | **Ineligible.** `textdistance.rs` by the same author. |
| `d3-geo-voronoi` → Rust | **Ineligible.** `rust_d3_geo` covers it. |
| `Moment.js` → Rust | Effectively ported (`chrono`); ~11k LOC over cap; 123 locales is data entry. |
| `lodash` → Rust | ~20k LOC (cap ~8k); 300 unrelated functions, no shared core; `_.uniq` → `HashSet` is not a finding. |
| `jQuery`, `D3` | DOM-dependent. Organizer guidance: "you can't diff what you can't reproduce." |
| `picomatch` → Rust | Output contract is a **JS RegExp source string**; ~320 KB of extglob tests exercise nested-lookahead backtracking Rust's `regex` cannot express. |
| `qs` → Rust | Tests mutate `Object.prototype` at runtime and assert sparse-array holes, `Object.create(null)`, `__proto__`, integer-key order — none survive a JSON boundary, which is where the harness lives. |
| `TinyColor` → Rust | Test suite is **Deno**, fetching `deno.land/std` over the network; float formatting *is* the library. |
| `mnemonist` → Rust | 6–9k LOC of ~30 unrelated stateful containers; worst possible differential-fuzz target (operation sequences, not pure functions). |
| `bytes.js` → Rust | Genuinely eligible and the safe pick — ~110 lines, real `toFixed` divergence. Rejected only because the story ceiling is low. |
| `martinlindhe/unit` → Rust | Thin wrapper; differential fuzzing reduces to comparing `x * 0.0254`, IEEE-identical by definition. |

## D2 — Target language: Go, not Rust

The pool lists this repo under **Track C (TypeScript → Go)** and again under
Track H as "TypeScript → Go." Both say Go.

Track H is defined as any pair not covered by A–G, and TS→Rust is not covered, so
TS→Rust was arguably available. **Rejected** — it contradicts the organizers' own
classification of this specific repo for a self-serving reason.

**Cost accepted.** Rust's `chrono-tz` returns `LocalResult::{Single, Ambiguous,
None}`, which makes ignoring DST ambiguity a compile error. Go has no equivalent —
`time.Date` silently normalizes nonexistent times and picks one interpretation for
ambiguous ones. So the "the type system makes the bug unrepresentable" argument is
**not available** in Go.

What replaces it is better: Go collapses the distinction too, just differently.
The finding is that *neither* language forces the question — only the differential
harness did. That locates correctness in testing discipline rather than in a
language feature, which is a more honest claim.

Also dropped: the **+5 Zero Unsafe** bonus does not map to a Go target.

## D3 — Equivalence is defined on the absolute instant

A fire time can be compared as a wall-clock string or as an absolute instant.
These disagree exactly at DST transitions, which is the entire subject.

**Decision:** the comparison key is **epoch milliseconds**. Local time and UTC
offset are also emitted, for diagnosis, but never for the pass/fail decision.

**Why.** A scheduler acts on instants. "Did the job run at the right moment" is
the operational question; "did the wall clock read 01:30" is a display concern.
Comparing wall-clock strings would call Go's double fall-back fire a match with
TypeScript's single fire, since both read `01:30`.

## D4 — Bridge protocol: two processes, JSON over stdout

Each implementation reads `corpus/cases.json` and writes the same JSON shape.
`corpus/diff.mjs` compares.

**Why not FFI / embedded runtime.** The rules forbid linking the source-language
runtime or FFI into the original interpreter. Two independent processes exchanging
data files sidesteps this entirely, and each emitter is independently runnable, so
a divergence can be attributed without re-running the other side.

**Consequence accepted:** the corpus can only express what survives JSON —
`(expr, tz, from, n) → [instant]`. Callback-shaped or stateful API surface is out
of scope. For cron next-fire this costs nothing, since the function is total and
deterministic. This was a deliberate criterion when picking the repo.

## D5 — The TS emitter is dependency-free

`corpus/emit_ts.mjs` derives local time and UTC offset via `Intl.DateTimeFormat`
rather than luxon, even though upstream depends on luxon.

**Why.** Keeps no third-party formatter in the measurement path, and lets the
emitter live outside `upstream/` without inheriting its `node_modules`. Upstream's
own luxon use is untouched — only the *observation* of results is independent.

## D6 — tzdata skew is measured, not assumed away

Node 24.7.0 ships ICU 77.1 / **tzdata 2025b**; Go 1.26.3 embeds **tzdata 2025c**.
Different releases, so any divergence could be database skew rather than library
behavior.

**Decision:** cross-check offsets on both sides of every transition in the corpus
before trusting any result. All six agree exactly
(`goprobe/offsets.go` vs `upstream/_probe/tzfacts.mjs`).

Go pins its database explicitly with `import _ "time/tzdata"` so results do not
depend on the host OS zoneinfo.

**Open risk:** this validates the transitions currently in the corpus. Expanding
the zone set requires re-running the check.

## D7 — Upstream tree stays byte-identical

`git status --porcelain` in `upstream/` reports **only** the untracked `_probe/`
directory. Zero tracked-file modifications. All probes are new files; nothing in
`src/` or `tests/` is touched.

Baseline: **302/302 tests pass** under `TZ=UTC`.

**Discovered constraint:** bare `npx jest` fails 17/302 on a machine in
`Asia/Kolkata`. Every upstream test script wraps in `cross-env TZ=UTC`. This is a
property of the suite, not a defect — but it means the Go port must pin the
process timezone identically, and must never let `time.Local` leak into schedule
computation. `robfig/cron` defaults to `time.Local` when no location is given,
making this a live hazard.

## D8 — Report the retraction rather than quietly fix it

Two claims I carried into this investigation turned out to be false, and both are
recorded in `FINDINGS.md` rather than deleted:

1. **`robfig/cron` uses AND for DOM/DOW.** Sourced from
   [zslayton/cron#141](https://github.com/zslayton/cron/issues/141) — a *Rust*
   crate — and assumed to generalize. It does not: `1 2 3 * 5` is byte-identical
   and POSIX-correct `OR` in both implementations.
2. **The `diff === 2` branch's behavior on Lord Howe.** I asserted it twice and
   was wrong twice — first "fires with the wrong magnitude," then "fires zero
   times." Both generalized from one probe. The sweep in `_probe/verify1b2.mjs`
   shows it is **start-minute dependent**: entering at `01:00`–`01:29` gives
   `diff = 1` (no fire), entering at `01:30`–`01:59` gives `diff = 2` (fires,
   recording an hour value for a half-hour gap).

**Why keep them.** Both were corrected by running code against an assumption, and
that is the thesis of the whole exercise. #1 in particular shows the failure mode
this project is about: reasoning from another ecosystem's issue tracker produced a
confident false claim that survived several restatements until measured.

## D9 — Regression corpus now, generated fuzzing later

`corpus/cases.json` is **15 hand-picked cases**, chosen by reading the DST code
and targeting its assumptions. It is a regression corpus.

The bonus criterion (+5) is 60 s of continuous zero-divergence fuzzing on a shared
API, which needs randomized generation over
`(expression grid × zone × start instant)`. **Not done.** Claiming the hand-picked
corpus as fuzzing would misrepresent it.

Deliberate sequencing: a targeted corpus that finds 3 real divergences in 15 cases
is more informative per case than random search, and it defines the oracle shape
the fuzzer will reuse.

## D10 — Reproduce upstream's DST bug rather than fix it

The port carries `applyDateOperation`'s `diff === 2` wall-clock check verbatim
([`crondate.go`](port/cronparser/crondate.go), `ApplyDateOperation`), including
the `dstStart`/`dstEnd` hints typed as *hours*. So the Go port **also** skips
March 29 in `Antarctica/Troll` and October 4 in `Australia/Lord_Howe`.

**Why.** The deliverable is a port, judged on behavioral equivalence. A port that
"fixes" the bug diverges from upstream on every affected zone, which is the
opposite of the goal. The bug is documented in FINDINGS.md and reportable
upstream; the port's job is to be indistinguishable.

Same reasoning for three smaller quirks preserved deliberately:

- `isLastWeekdayOfMonthMatch` parses only `charAt(0)` of each value
- the `31 11 *` validation inspects `values[0]` only, so `1,31 11 *` passes
- `checkDstTransition` is blind to midnight-transition zones (see D11)

## D11 — One divergence the port does *not* reproduce

`prev()` on `0 0 0 * * *` in `Pacific/Chatham` from `2026-09-27T05:45:00` throws
`"Invalid expression, loop limit exceeded"` upstream. The port returns the correct
answer.

**Decision: do not reproduce.** Propagating a loop-limit hang into a rewrite is
not fidelity worth having, and the correct values are not in dispute — upstream
itself returns them when started from `01:00` instead of `05:45`.

Recorded in [`corpus/known-divergences.json`](corpus/known-divergences.json) with
a machine-readable predicate, so the fuzzer counts it separately and it can never
silently mask a *new* divergence. Regression-tested in
[`port/cronparser/chatham_test.go`](port/cronparser/chatham_test.go).

Scope: needs a `:45`-offset zone AND its spring-forward day AND a target hour
below the gap. Hours 0 and 1 throw; 22 and `11,22` do not.

## D12 — Transcribe luxon's `fixOffset` instead of inferring a rule

Ambiguous (fall-back) start times cost more debugging than everything else
combined. Five separate empirical rules were tried and each failed on some zone,
because **luxon's tie-break depends on the offset at the moment the code runs**
(`guessOffsetForZone` seeds `fixOffset` from `Settings.now()`).

**Decision:** transcribe `fixOffset` from luxon's source directly, including its
`Math.min`/`Math.max` hole-time branch, and seed it from `time.Now().In(loc)`.

**Why it matters as a decision, not just a fix.** No amount of black-box
differential testing would have found this — the behavior is not a function of
the inputs alone. It took reading the dependency's source. That is a real limit on
what differential fuzzing can prove, and it is worth saying so rather than
implying the harness found everything.

Cost accepted: the port inherits a time-dependent answer for ambiguous start
times. That is upstream behavior. It is also the finding most worth reporting.

## D13 — Sub-hour arithmetic: which reconstruction preserves the offset

Go's `time.Date` and luxon's `set`/`startOf`/`endOf` disagree about ambiguous and
nonexistent wall clocks, and **not uniformly** — the right answer differs per
operation. Settled empirically per method, each documented at its definition:

| operation | reconstruction | reason |
| --- | --- | --- |
| `setLocal` (set*) | preserve current offset | luxon's `set()` stays on the current side |
| `addHour`/`addMinute` | preserve current offset | matches `startOf` after an absolute add |
| `subtractHour`/`subtractMinute` | **compute at current offset via UTC** | luxon's `endOf` boundary is in the held offset, which on Lord Howe's 30-min fall-back lands an hour *earlier* and skips the repeated hour |
| `addDay`/`subtractDay` | UTC calendar arithmetic, then clamp | `AddDate` on a midnight-transition zone moves backwards (D-Santiago) |
| `addMonth`/`subtractMonth` | month arithmetic, never `AddDate` | `AddDate` normalizes Jan 31 + 1mo to Mar 3 |
| `endOfDayIn` | prefer the **later** occurrence | luxon's `endOf('day')` is the true end of day |

Each row is a place where the obvious Go translation is wrong. None is
discoverable by reading the TypeScript.

## D14 — Ship the residual divergence documented rather than hidden

`prev()` from an ambiguous instant in a sub-hour-transition zone diverges at
~1 per 30,000 cases (FINDINGS.md §4g). The cause is understood: luxon's
`endOf(unit)` is `startOf(unit).plus({unit:1}).minus(1ms)`, which mixes
wall-clock truncation with absolute addition, and on a 30-minute fall-back that
composition lands an hour earlier than reconstructing "hour N at :59:59".

I attempted the faithful transcription and it made iteration **stall** —
`01:59:59 +10:30` repeating until the loop limit. Several offset-seeding variants
either stalled or reproduced the original one-hour error.

**Decision:** keep the formulation with the longest clean fuzz run, document the
limitation at the function definition and in FINDINGS/README, and state the
residual rate explicitly.

**Why not keep grinding.** The remaining options were (a) more blind variants, or
(b) transcribing enough of luxon's `Duration`/`objToTS`/`normalizeUnit` machinery
to be certain — effectively porting a second library. Neither is a good trade
against a characterised edge case affecting one direction in two zones. Judges can
weigh a known, scoped, reproduced gap; they cannot weigh one I concealed.

---

## Status and what is *not* done

**Done.** Repo selection with eligibility evidence · upstream pinned at
`8410d37`, its suite untouched and green (302/302, `TZ=UTC`, zero tracked-file
changes) · #419 reproduced against the public API · PR #435 shown incomplete for
sub-hour transitions · TS/Go fall-back disagreement · tzdata skew controlled ·
**the Go port: 2,234 LOC, zero dependencies** · six Go-vs-luxon semantic
mismatches found and fixed · randomized differential fuzzing (6,400 cases over 10
seeds; 36,040 in a 180.8 s continuous run) · a previously unreported upstream
`prev()` loop-limit bug · benchmark report · findings and decisions written.

**Not done.** The 5-minute demo video · upstream issue reports (three are drafted
in FINDINGS.md but not filed) · closing the §4g residual divergence · differential
coverage for `Take()` with negative limits, `Reset()`, `IncludesDate()`, and the
`startDate`/`endDate` bounds (unit-tested only).

**Findings, in reportable form:**
1. #419's diagnosis is incomplete — the 30-minute case fails by a *different*
   mechanism (truncation to `diff = 1`) than the 2-hour case (`diff = 3`).
2. **PR #435 does not fix `Australia/Lord_Howe`** — `Math.floor(30/60) === 0`.
   The fix needs minute granularity; `dstStart`/`dstEnd` currently hold hours.
3. `robfig/cron` fires literal-hour jobs **twice** on fall-back where cron-parser
   fires once; both fire twice for wildcard hours. Undocumented on both sides.
