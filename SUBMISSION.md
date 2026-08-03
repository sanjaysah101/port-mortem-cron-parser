# Submission form — copy-paste answers

Form: https://tally.so/r/Npve6l

---

## Team Name *

```
Sanjay Kumar Sah
```

*(Solo entry. If you'd rather have a team name than your own, pick something and
use it consistently — it goes on the leaderboard.)*

## Project Name *

```
cronparser-go — cron-parser (TypeScript) → Go
```

## Participants Discord Username(s) *

```
<your Discord username>
```

## LinkedIn

```
<your LinkedIn URL, or leave blank>
```

## Code *

```
https://github.com/sanjaysah101/port-mortem-cron-parser
```

## Presentation or Video

```
https://youtu.be/4O2HAY4QAoM
```

## Demo

```
https://github.com/sanjaysah101/port-mortem-cron-parser#run-it
```

*(There's no hosted click-and-play app — it's a library. The README anchor lands
judges directly on the one-command reproduction steps, which is the closest
honest equivalent.)*

---

## Description *

**Short version** (use this if the field is cramped):

> TypeScript → Go port of `harrisiirak/cron-parser` (~30M downloads/month).
> Track C. 2,823 LOC TS → 2,234 LOC Go, zero dependencies.
>
> The original 302 Jest tests can't execute against a Go binary, and FFI into
> Node is banned — so I used the suite as an **oracle** instead of translating
> it: extracted all 130 literal `parse()` call sites from the unmodified
> `.test.ts` sources, recorded what the real TypeScript returns, replayed against
> the port. **130/130 match.** Plus 6,400 generated cases over 10 seeds and
> 180.8s / 36,040 cases of continuous differential fuzzing, zero unexplained
> divergence. Upstream suite untouched: 302/302, zero tracked-file changes,
> SHA-256 manifest committed.
>
> Found 7 places where Go's `time` and luxon silently disagree — including one
> that black-box fuzzing **structurally could not** find: luxon resolves an
> ambiguous local time using `guessOffsetForZone()`, seeded from
> `Settings.now()`. So `parse()` with an ambiguous timestamp returns an instant
> an hour apart depending on **what season your process is running in.** Five
> empirical rules failed before I read the dependency's source.
>
> Also found a previously unreported upstream bug: `prev()` throws
> "loop limit exceeded" on `0 0 0 * * *` in `Pacific/Chatham`. The port
> deliberately does *not* reproduce it.
>
> One residual divergence at ~1 per 30,000 is documented, not hidden. Two of my
> own wrong claims are kept in the write-up, because being corrected by running
> the code is the whole point.

---

**Long version** (use if the field accepts a few paragraphs):

> **What it is.** A Go port of `harrisiirak/cron-parser`, the cron expression
> parser behind roughly 30M npm downloads a month. Track C (TypeScript → Go).
> 2,823 lines of TypeScript became 2,234 lines of Go with **zero dependencies** —
> upstream needs luxon, the port needs nothing outside the standard library.
>
> **The interesting problem isn't the port.** Generating a port is easy. The
> hackathon's own framing is right: proving it behaves the same is the open
> problem. So most of this submission is about that.
>
> **How the original test suite is used.** The 302 tests are Jest, in TypeScript,
> calling `expect()`. A Go binary cannot execute them, and making Jest drive Go
> would need FFI into Node — banned by the rules. Translating them into Go tests
> would produce a suite that no longer matches what the judges hashed, so a diff
> would prove nothing.
>
> Instead I used the suite as an **oracle**. A recorder reads the unmodified
> `.test.ts` files *as text*, extracts all 130 literal
> `CronExpressionParser.parse(expression, options)` call sites, runs each against
> the real TypeScript implementation, and records the outcome — exact error string,
> or 8 forward and 8 backward fire times as epoch milliseconds. The Go port
> replays them and diffs. **130/130 match** (52 with full fire-time comparison;
> 78 parse-only, because those call sites iterate from "now" and can't be
> replayed by construction — I report those separately rather than counting them
> as passes).
>
> That replay caught three port bugs my generated fuzzer structurally could not,
> because the fuzzer always passed an explicit timezone. The biggest: upstream
> interprets an expression with no `tz` option in the **process** timezone, and I
> had defaulted to UTC. 91 of 130 call sites were diverging by exactly 5h30m.
>
> **Equivalence evidence.** Original suite as oracle: 130/130. Pinned corpus:
> 15/15. Generated corpus, 10 seeds × 640: 6,400/6,400. Continuous differential
> fuzz: 180.8 seconds, 36,040 cases, **zero unexplained divergence** (the bonus
> threshold is 60s). The upstream suite itself is untouched — 302/302 under
> `TZ=UTC`, zero tracked-file modifications, and SHA-256 of all 24 upstream files
> is committed so anyone can verify the tree is the one the port was validated
> against. I confirmed the manifest regenerates byte-identical on a fresh clone.
>
> **The finding I'd most want a maintainer to see.** My fuzzer kept producing
> divergences of exactly one DST shift on ambiguous times — the repeated hour
> during fall-back. I tried five rules for which of the two valid instants luxon
> picks: earliest, latest, larger offset, pre-transition, post-transition. Every
> one failed on some zone.
>
> There is no rule. `fixOffset` in luxon's source is a guess-and-correct search,
> and its own comment says the guess *"determines which offset we'll pick in
> ambiguous cases."* For IANA zones that guess is `guessOffsetForZone(zone)` —
> **the zone's offset at `Settings.now()`**. So calling `parse()` with an
> ambiguous timestamp can return an instant an hour apart depending on what
> season the process happens to be running in. That's a latent hazard for anyone
> scheduling across a fall-back, and **no amount of black-box differential
> testing would find it**, because the behaviour isn't a function of the inputs.
> I had to read the dependency's source. That's a real ceiling on what
> differential fuzzing can prove, and I'd rather say so than imply the harness
> found everything.
>
> **Bug found upstream.** `prev()` throws `"loop limit exceeded"` on
> `0 0 0 * * *` in `Pacific/Chatham` — a plain daily-midnight schedule. Also
> reproduces in `Australia/Lord_Howe`. Previously unreported; found by the fuzzer,
> then minimised by hand. The port deliberately does *not* reproduce it, and it's
> registered with a narrow predicate so it can never mask a real port bug. Two
> further reports are drafted: #419's diagnosis is incomplete (the 30-minute case
> fails by a different mechanism than the 2-hour case), and the open fix in PR
> #435 computes `Math.floor(30/60) = 0`, so it doesn't fix `Australia/Lord_Howe`.
>
> **What doesn't work.** One residual divergence at about 1 per 30,000:
> `prev()` from an ambiguous instant in a sub-hour-transition zone. I found the
> cause (luxon's `endOf` is `startOf().plus().minus(1ms)`, mixing wall-clock
> truncation with absolute addition), attempted the faithful fix, and it made
> iteration stall instead. It's documented at the function and in FINDINGS.md §4g
> rather than hidden. Benchmarks show 35× faster mean and 5× less memory, with
> the caveat stated *before* the numbers that Go's clock on Windows has ~1ms
> granularity so these are batch means rather than per-call tails — and that part
> of the gap is luxon-vs-stdlib, not Go-vs-TypeScript.
>
> Two claims I got wrong along the way are still in the write-up, because being
> corrected by running the code is the entire point of the exercise.
>
> Docs: README, FINDINGS, DECISIONS (D1–D14), CONFORMANCE, BENCHMARKS.
> Single-command build: `node run.mjs`.

---

## Notes before you submit

- **Verify the video is not Private.** Open https://youtu.be/4O2HAY4QAoM in an
  incognito window. If it says "Video unavailable", switch it to Unlisted or
  Public in YouTube Studio. This is the one thing that silently sinks a
  submission.
- The form has **no track field**, so state "Track C (TypeScript → Go)" inside the
  Description — both versions above do.
- The rules mention an advisory disk layout with `.port-mortem.toml`. This repo
  uses a different layout (`port/`, `corpus/`, `conformance/`), which the rules
  describe as advisory rather than mandatory. The README explains the layout.
