# Port Mortem 2026 — cron-parser → Go

Track C (TypeScript → Go). Target:
[`harrisiirak/cron-parser`](https://github.com/harrisiirak/cron-parser) @
`8410d37`, 2,823 LOC TypeScript.

**The port is written and behaviorally equivalent under differential fuzzing**
(with one documented exception). 2,823 LOC of TypeScript → **2,234 LOC of Go**
plus 550 LOC of tests. Standard library only, **zero dependencies** — upstream
needs luxon.

| harness | cases | unexplained divergences |
| --- | --- | --- |
| hand-picked corpus | 15 | **0** |
| generated corpus, 10 seeds × 640 | 6,400 | **0** |
| continuous fuzz, best run (180.8 s) | 36,040 | **0** |

One residual edge case remains, at roughly **1 per 30,000**: `prev()` from an
ambiguous instant in a sub-hour-transition zone. Characterised, reproduced, and
documented in [FINDINGS.md](FINDINGS.md) §4g rather than papered over — see
*Honest limits* below.

Upstream's own suite is untouched and green: **302/302** under `TZ=UTC`, with
`git status` showing zero tracked-file modifications and SHA-256 of every test
file recorded in [`conformance/upstream-hashes.json`](conformance/upstream-hashes.json).

**The original suite is used as an oracle, not translated.** Its 130 literal
`parse()` call sites are extracted from the unmodified `.test.ts` sources, executed
against the real TypeScript implementation to record what it returns, then replayed
against the port: **130/130 match** (52 with full fire-time comparison, 78
parse-only because they iterate from "now" and cannot be replayed by construction).
That replay found three port bugs the corpus fuzzer structurally could not see —
including the biggest one, a wrong default timezone. See
[CONFORMANCE.md](CONFORMANCE.md).

## Why this repo

POSIX cron predates the tz database and says **nothing** about daylight saving
time. So for the cases that matter most, there is no specification to appeal to —
"behaviorally equivalent" is undefined exactly where implementations disagree. The
port is what forces the question into the open.

Three implementations, three answers, all reproducible below.

## Findings

**1. `diff === 2` is wall-clock arithmetic** —
[`CronDate.ts:558`](upstream/src/CronDate.ts#L558) detects DST by subtracting hour
*numbers*. Two ways to fail:

- `Antarctica/Troll` jumps **2 h**: `diff = 3`, overshoots the constant.
- `Australia/Lord_Howe` jumps **30 min**: `diff` is `1` *or* `2` depending on the
  minute iteration entered the hour at — the observation is entry-point dependent,
  so no constant can be right.

Either way a daily job **silently skips a day**. This is upstream
[#419](https://github.com/harrisiirak/cron-parser/issues/419).

**2. The open fix for #419 does not fix the 30-minute case** (novel).
[PR #435](https://github.com/harrisiirak/cron-parser/pull/435) proposes
`Math.floor((offset - prevOffset) / 60)`. For Lord Howe that is
`floor(30/60) = 0` — zero compensation for a real 30-minute gap. `dstStart` and
`dstEnd` are typed to hold **hours**, so no constant fixes this; the unit must be
minutes.

**3. TypeScript and Go disagree on the repeated hour** (novel). A daily
`30 1 * * *` job across `America/New_York`'s fall-back:

| | Nov 1 (1st pass) | Nov 1 (2nd pass) |
| --- | --- | --- |
| cron-parser (TS) | `01:30 -04:00` | — |
| robfig/cron (Go) | `01:30 -04:00` | **`01:30 -05:00`** |

**Go runs the nightly job twice.** Both libraries also fire twice for wildcard
hours (`0 * * * *`), so each is internally inconsistent between literal and
wildcard hour fields — and neither documents the rule.

**4. Six ways Go's `time` package silently disagrees with luxon** — found by the
harness while building the port, not by reading code. Field padding; ambiguous
times (Go picks the first occurrence, luxon keeps the current offset); month
overflow (`Jan 31 + 1mo` → Go `Mar 3`, luxon `Feb 28`); midnight DST transitions
where `AddDate` moves *backwards* and iteration stalls forever; a validation
hiding in the collection constructor; and —

**5. luxon's answer for an ambiguous time depends on when you run it.** The best
finding. `fixOffset` is a guess-and-correct search seeded by
`guessOffsetForZone`, which returns the zone's offset at **`Settings.now()`**. So
`parse(..., {currentDate: <ambiguous wall clock>})` can return an instant an hour
apart depending on the season the process runs in. Not inferable from outputs —
five empirical rules were tried and each failed on some zone. Only reading
luxon's source explained it. **That is a real limit on what black-box differential
fuzzing can prove.**

**6. A claim I had to retract.** I asserted `robfig/cron` uses `AND` for
day-of-month/day-of-week, from
[zslayton/cron#141](https://github.com/zslayton/cron/issues/141) — a *Rust* crate.
It does not; `1 2 3 * 5` is byte-identical POSIX-correct `OR` in both. Kept in the
write-up because it is the exact failure mode this project is about: reasoning from
another ecosystem's issue tracker produced a confident false claim that survived
several restatements until it was measured.

**7. One divergence the port refuses to reproduce.** `prev()` throws
`"loop limit exceeded"` upstream when iterating backward across a spring-forward
gap in irregular zones — confirmed in `Pacific/Chatham` (`+12:45`) and
`Australia/Lord_Howe` (30-minute shift). The port returns the correct answers.
Recorded in [`corpus/known-divergences.json`](corpus/known-divergences.json) with a
deliberately narrow predicate, so it can never mask a real port bug.

Full detail, with provenance and reproduction commands: [FINDINGS.md](FINDINGS.md).
Benchmarks (35× faster mean, 5× less memory, with an honest methodology caveat):
[BENCHMARKS.md](BENCHMARKS.md).

## Run it

Requires Node ≥ 24, Go ≥ 1.26, git.

```bash
node run.mjs            # setup + baseline + probes + differential
```

`run.mjs` is the portable entry point and needs no build tooling beyond Node —
it was used to verify every command below on Windows, where `make` is often
absent. A `Makefile` with identical targets is provided for Unix habits.

Step by step:

```bash
node run.mjs setup        # clone @ 8410d37, npm ci, build, go mod tidy
node run.mjs baseline     # upstream suite: 302/302 (TZ=UTC), tree unmodified
node run.mjs hashes       # SHA-256 every upstream test + source file
node run.mjs conformance  # ORIGINAL SUITE as oracle -> 130/130 match
node run.mjs test         # the port's own Go tests
node run.mjs port         # PORT vs original on pinned corpus -> 15/15
node run.mjs generated    # 5 seeds x 640 generated cases
node run.mjs fuzz 180     # continuous differential fuzz (bonus needs 60s)
node run.mjs bench        # latency / memory on both sides
node run.mjs probes       # reproduce each investigation finding
node run.mjs diff         # cron-parser vs robfig/cron -> 12/15 (investigation)
```

Note `diff` compares the *original* against `robfig/cron` (the pre-port
investigation, where 3 of 15 diverge); `port` compares the original against **this
port**, where all 15 agree.

## Layout

```
port/                       THE PORT
  cronparser/
    parser.go               expression -> fields (aliases, ranges, steps, H, L, #n)
    field.go                field values, wildcard/L detection, nearest-value search
    crondate.go             date arithmetic — where all six mismatches live
    expression.go           the iteration loop, DST hints, POSIX dom/dow rules
    random.go               xfnv1a + mulberry32, bit-exact vs upstream
    *_test.go               unit tests + regression guards per finding
  cmd/emit/                 conformance emitter (shared JSON shape)
  cmd/bench/                benchmark harness

upstream/                   cron-parser @ 8410d37 — TRACKED FILES UNMODIFIED
  _probe/                   new, untracked: finding reproductions

corpus/
  cases.json                15 hand-picked cases
  gen.mjs                   deterministic generator (16 zones, next + prev)
  fuzz.mjs                  continuous differential fuzzer
  emit_ts.mjs               TypeScript emitter (dependency-free)
  diff.mjs                  compares emitter outputs on epoch ms
  known-divergences.json    the one tolerated divergence, with its predicate

goprobe/                    robfig/cron comparison (pre-port investigation)
bench/bench_ts.mjs          TypeScript benchmark

FINDINGS.md                 all findings + two retractions, with provenance
DECISIONS.md                why this repo, why Go, D10-D13 on port tradeoffs
BENCHMARKS.md               latency / memory / startup, with caveats
run.mjs                     portable task runner (no `make` needed)
```

## How equivalence is defined

Comparison is on **epoch milliseconds**, not wall-clock strings — they disagree
precisely at DST transitions, which is the subject. Local time and offset are
emitted for diagnosis only. Comparing strings would call Go's double fire a match,
since both read `01:30`.

The bridge is **two processes exchanging JSON** — no FFI, no embedded
source-language runtime. Each emitter runs independently, so a divergence can be
attributed without re-running the other side.

## Provenance

| | |
| --- | --- |
| upstream | `8410d3717b7adda1e5b9c5fd6c40cb2cbf9d52e4`, v5.7.0 |
| Node | v24.7.0, ICU 77.1, **tzdata 2025b** |
| Go | go1.26.3, **tzdata 2025c** (`time/tzdata`) |
| robfig/cron | v3.0.1 |

Node and Go ship **different tzdata releases**, so offsets on both sides of all
six transitions were cross-checked and are identical. The divergences are library
behavior, not database skew.

## Honest limits

- **The port is not fully equivalent.** `prev()` from an ambiguous instant in a
  sub-hour-transition zone (`Australia/Lord_Howe`, `Pacific/Chatham`) diverges at
  ~1 per 30,000 cases. Cause identified (luxon's `endOf` is
  `startOf().plus().minus(1ms)`, mixing wall-clock and absolute arithmetic);
  faithful transcription attempted and made iteration stall instead. Scope and
  reproduction in [FINDINGS.md](FINDINGS.md) §4g.
- **Differential fuzzing cannot prove this.** Finding 5 is a behavior that depends
  on when the code runs, not on its inputs. Black-box diffing found the *symptom*
  (intermittent one-hour divergences) but only reading luxon's source explained it.
  A harness that agrees for 180 s is evidence, not proof.
- The fuzzer covers `Parse` + `Next`/`Prev`. `Take()` with negative limits,
  `Reset()`, `IncludesDate()`, and the `startDate`/`endDate` bounds have unit tests
  but are **not** in the differential harness.
- Date window is 2026–2028 and the zone list is 16 of ~600. Zones with historical
  oddities (pre-1970 offsets, LMT, zones that changed *rules* mid-year) are
  untested.
- Benchmarks are **batch means, not per-call tails** — Go's clock on Windows has
  ~1 ms granularity. See the methodology note in [BENCHMARKS.md](BENCHMARKS.md).
  Some of the 35× gap is luxon-vs-stdlib, not Go-vs-TypeScript.
- **No demo video.**
- Findings are novel **as far as I searched**; absence of a prior report is not
  proof of novelty.
- Upstream's own 302 tests validate the *original*, not the port. The port has its
  own Go tests; it does not execute the TypeScript suite.
