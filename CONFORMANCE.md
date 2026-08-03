# CONFORMANCE — how the original test suite is used against the port

## The problem

The rules ask for "the original test suite, hashed at kickoff, passing against
your port." For a TypeScript → Go port that is not literally possible:

- The 302 tests are **Jest tests written in TypeScript**. They `import` from
  `../src/CronExpressionParser` and call `expect(...)`. A Go binary cannot execute
  them.
- Making `npx jest` drive Go code would require **FFI into Node**, which the rules
  explicitly ban ("No source-language runtime — no FFI into the original
  interpreter").
- **Translating** the 302 tests into Go tests would produce a suite that no longer
  matches what the judges hashed, so a diff against the original proves nothing.

So there are three options, and only one is honest.

| approach | verdict |
| --- | --- |
| Run Jest against the Go port | Impossible without banned FFI |
| Hand-translate the tests to Go | Not the original suite; defeats the hash check |
| **Record what the original does at every test call site, replay against the port** | ✅ what this repo does |

## What we do

Two steps, neither of which edits a test file.

**1. Record** — [`conformance/capture.mjs`](conformance/capture.mjs) reads
`upstream/tests/*.test.ts` **as text**, extracts every literal

```js
CronExpressionParser.parse('<expression>', { ...options })
```

call site, and executes each against the **real TypeScript implementation**,
recording the outcome: the thrown error message, or the first 8 `next()` and 8
`prev()` results as epoch milliseconds.

**2. Replay** — [`port/cmd/conformance`](port/cmd/conformance/main.go) runs the
same calls through the Go port and diffs on absolute instants and exact error
strings.

```bash
node run.mjs conformance
```

## Result

```
call sites   : 130
  match                      52
  parse-only (unanchored)    78
RESULT: 130/130 match (100.0%)
```

- **52 fully replayed** — parse outcome *and* 8 forward + 8 backward fire times
  compared as epoch milliseconds.
- **78 parse-only** — see the limitation below. Their parse outcome (success or
  the exact error string) *is* compared; their fire times are not comparable.

The test files remain byte-identical, which is verified independently:

```bash
node run.mjs baseline
# Tests: 302 passed, 302 total   (TZ=UTC)
# git status --porcelain -> only untracked _probe/
```

## What this proves, and what it does not

**Proves:** for every literal `parse()` call the upstream suite makes, the port
agrees with the original — same success/failure, same error text
character-for-character, and for anchored cases the same fire instants.

**Does not prove:** that the port would pass the 302 Jest tests. Those tests also
assert on things the trace does not capture — field-collection internals,
`stringify()` output, `CronDate` unit behaviour, `CronFileParser` file handling.
The replay covers the *parser and iterator surface*, which is the substance, not
the whole suite.

### The 78 unanchored call sites

A call site with no `currentDate` or `startDate` iterates from **now**. Its fire
times are a function of the wall clock at record time, so replaying them later can
never match by construction — the recording would be comparing 2026-08-03 against
whenever the replay runs.

These are reported as `parse-only (unanchored)` rather than counted as passes.
Marking them "match" would be inflating the number; excluding them from the total
would be hiding them. The parse outcome for each *is* still compared, which is a
real check — 31 of the 130 call sites are error cases, and all 31 match.

### `TZ` must be pinned

The trace records the timezone it was captured under (`recordedUnderTZ: "UTC"`),
and the replayer sets `time.Local` to match. This is not incidental: cron-parser
interprets an expression with no `tz` option in the **process timezone** (luxon
falls back to the system zone), so recording under UTC and replaying under
`Asia/Kolkata` produced 91 spurious divergences of exactly 5h30m.

Upstream has the same constraint — every one of its test scripts is wrapped in
`cross-env TZ=UTC`, and running bare `npx jest` in a non-UTC zone fails 17/302.

## Three port bugs this found

The replay was not a formality. It caught real defects the generated-corpus fuzzer
had missed, because the fuzzer always passed an explicit `tz` and `currentDate`:

1. **`time.Local` vs UTC default.** The port defaulted to UTC when no `tz` was
   given; upstream defaults to the system zone. 91/130 call sites diverged by the
   host offset. This is the single highest-impact bug found in the whole project
   and the corpus fuzzer structurally could not see it.
2. **Standalone `L` in dayOfWeek.** `0 0 0 * * L` and `0 0 0 * * 1,L` must throw
   `"CronDayOfWeek Validation error, unexpected standalone L"` — the suffix form
   (`5L`) is legal, a bare `L` is not. The port accepted both.
3. **`parseInt("")` → `NaN` in error text.** `-1 * * * * *` splits to `["", "1"]`,
   and JS reports `"got range NaN-1"`. The port reported `"got range -1"`. Upstream's
   own tests assert on these strings, so the text has to match exactly.

None of the three were reachable from the differential corpus. That is the
argument for doing this in addition to fuzzing rather than instead of it.
