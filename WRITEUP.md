# The bug that differential fuzzing cannot find

*Porting a 30M-download-a-month cron parser from TypeScript to Go, and the one
thing 36,000 randomized test cases could never have told me.*

---

I spent a weekend porting [`cron-parser`](https://github.com/harrisiirak/cron-parser)
— the library behind roughly 30 million npm downloads a month — from TypeScript
to Go. 2,823 lines became 2,234 with zero dependencies.

The port isn't the interesting part. Any competent AI agent can make a port
compile in an afternoon. The interesting part is what happened when I tried to
*prove* the two implementations behave identically, and hit a wall that no amount
of randomized testing could have gotten me over.

## First, an honest problem: you can't run the tests

The hackathon rules say the original test suite should pass against your port.
For a TypeScript → Go port that's not literally possible:

- The 302 tests are **Jest tests, in TypeScript**, calling `expect()`. A Go binary
  cannot execute them.
- Making Jest drive Go code needs **FFI into Node** — explicitly banned.
- **Translating** the tests to Go gives you a suite that no longer matches what
  the judges hashed. A diff against it proves nothing.

So I used the suite as an **oracle** instead of translating it. A recorder reads
the unmodified `.test.ts` files *as text*, extracts every literal

```js
CronExpressionParser.parse('<expression>', { ...options })
```

call site — 130 of them — runs each against the real TypeScript implementation,
and records the outcome: the exact error string, or the next 8 and previous 8 fire
times as epoch milliseconds. Then the Go port replays them and diffs.

130/130 match. The test files are never touched — verified independently by
running them (302/302) and by committing SHA-256 of all 24 upstream files.

That replay immediately caught three bugs my fancy fuzzer had missed. The worst
one: upstream interprets an expression with no `tz` option in the **process
timezone**, because luxon falls back to the system zone. I'd defaulted to UTC. 91
of 130 call sites were quietly off by exactly 5 hours 30 minutes — the offset of
the machine I was sitting at.

**Lesson one: the original tests know things your generated corpus doesn't.** My
fuzzer always passed an explicit timezone, so it structurally could not see that
bug. Not "didn't happen to" — *could not*.

## Then, the fun part: DST is not one hour

I built a differential fuzzer: generate a random corpus, run both
implementations, diff on absolute epoch milliseconds, repeat until something
disagrees. 16 timezones, chosen to cover every DST transition *width* that
actually exists:

| zone | transition width |
| --- | --- |
| `America/New_York` | 60 min (the one everybody assumes) |
| `Antarctica/Troll` | **120 min** |
| `Australia/Lord_Howe` | **30 min** |
| `Pacific/Chatham` | 60 min, but at a `+12:45` base offset |
| `America/Santiago` | 60 min, **at midnight** |

That list is why the port took a weekend instead of an afternoon. Six separate
places where Go's `time` package and luxon silently disagree, none of them
visible by reading the TypeScript, because they don't exist *in* the TypeScript —
they exist at the boundary between two languages' notions of "the same time":

**Ambiguous times.** During fall-back, 01:30 happens twice. Go's `time.Date`
always picks the first occurrence; luxon keeps the offset it already had. My
iteration loop couldn't escape the repeated hour and spun to its loop limit.

**Month overflow.** `Jan 31 + 1 month` is **Mar 3** in Go (it normalizes Feb 31
forward) and **Feb 28** in luxon (it clamps). February got skipped entirely, so
`L 2 *` — "last day of February" — never matched anything, ever.

**Midnight transitions.** In `America/Santiago` the DST shift happens *at
midnight*, so on 2026-09-06 the local day starts at 01:00 and 00:00 doesn't
exist. `time.Date(2026, 9, 6, 0, 0, 0, 0, santiago)` returns
**`2026-09-05T23:00:00`** — the *previous day*. So `AddDate(0,0,1)` from Sep 5
midnight lands on the nonexistent Sep 6 00:00, resolves backward, and `.Day()` is
still 5. The loop asks for Sep 5 again. Forever.

Each one was found by the harness, not by reading code. That's the fuzzer earning
its keep.

## And then the wall

The fuzzer kept surviving about 70 seconds and then finding a divergence of
exactly one DST shift, on ambiguous start times. So I tried to work out the rule
for which of the two valid instants luxon picks.

| zone | ambiguous time | luxon picks |
| --- | --- | --- |
| `Antarctica/Troll` | 2026-10-25 01:15 | **earlier** |
| `Europe/Berlin` | 2026-10-25 02:30 | **earlier** |
| `America/New_York` | 2026-11-01 01:30 | **earlier** |
| `Pacific/Auckland` | 2026-04-05 02:30 | **later** |
| `Australia/Lord_Howe` | 2026-04-05 01:45 | **later** |

Earliest? Fails on Auckland. Latest? Fails on Troll. Larger offset? Smaller
offset? Pre-transition side? Post-transition side? I tried five rules. Every one
failed on some zone. Each attempt cost a code change, a rebuild, and a two-minute
fuzz run to disprove.

Eventually I stopped guessing and opened `node_modules/luxon/src/datetime.js`:

```js
// find the right offset a given local time. The o input is our guess, which
// determines which offset we'll pick in ambiguous cases (e.g. there are two
// 3 AMs b/c Fallback DST)
function fixOffset(localTS, o, tz) { ... }
```

The comment says it outright. It's a guess-and-correct search, and the *guess*
decides the answer. So what's the guess?

```js
function guessOffsetForZone(zone) {
  if (zoneOffsetTs === undefined) {
    zoneOffsetTs = Settings.now();     // <-- the current moment
  }
  ...
}
```

**The zone's offset right now.**

Read that again, because it took me a while to accept it. For an ambiguous local
timestamp — the repeated hour during fall-back — which of the two real instants
you get back **depends on what time it is when you call the function.** Run the
same `parse()` in January and in July, in a zone that observes DST, and the tie
breaks differently. Same inputs. Different output.

There was never a rule to reverse-engineer. My five failed hypotheses weren't bad
guesses; they were attempts to fit a function to data that isn't a function of
the inputs at all.

## The point

I had a differential harness that ran 36,040 cases across 106 rounds and 180
seconds without an unexplained divergence. By any reasonable standard that's
strong evidence of equivalence. And it **could not have found this**, ever, at any
scale — because differential fuzzing tests `f(input) == g(input)`, and this
behaviour isn't in `f(input)`. It's in the wall clock.

Randomized differential testing is the best tool I know for this problem. It
found six real bugs in my port, several of which I'd have shipped confidently.
But it has a *shape*: it can only find divergences that are reproducible from the
inputs. Hidden state — ambient time, locale, environment, a cached "now" from
process start — is invisible to it by construction.

The fuzzer told me *something* was wrong: intermittent one-hour divergences that
came and went. It could not tell me *why*, and no additional runtime would have.
Reading 40 lines of a dependency's source did in ten minutes what a week of
compute could not.

So my honest summary of the port isn't "proven equivalent." It's: **equivalent
across 6,400 generated cases over 10 fixed seeds, 130/130 recorded call sites
from the original suite, and a 180-second continuous run — with one known
unfixed edge case whose scope I've characterised and whose reproduction is
committed.** That's a weaker claim and a truer one.

## Two smaller things I'd rather admit than bury

**One residual divergence, about 1 in 30,000.** Backward iteration from an
ambiguous instant in a sub-hour-transition zone. I found the cause — luxon's
`endOf(unit)` is `startOf(unit).plus({unit:1}).minus(1ms)`, mixing a wall-clock
truncation with an *absolute* addition, which on a 30-minute fall-back lands an
hour earlier than you'd expect. I attempted the faithful transcription and it made
iteration stall instead. It's documented at the function and in the findings, not
hidden.

**Two claims I got wrong.** I asserted several times that Go's most popular cron
library uses AND for day-of-month/day-of-week where POSIX specifies OR. I'd
sourced that from an issue tracker for a *different library in a different
language* and never checked. It's OR. Correct. I also gave two different wrong
accounts of the `diff === 2` mechanism before a proper sweep settled it.

Both are still in the write-up. Getting corrected by running the code is the
entire point of the exercise; deleting the evidence would defeat it.

## Bonus: a bug in the original

While fuzzing, my port started *succeeding* where upstream threw. Minimised:

```js
CronExpressionParser.parse('0 0 0 * * *', {
  tz: 'Pacific/Chatham',
  currentDate: '2026-09-27T05:45:00',
}).prev();
// Error: Invalid expression, loop limit exceeded
```

`0 0 0 * * *` is "every day at midnight." Iterating *backward* across Chatham's
spring-forward gap exhausts the 10,000-step loop limit. Also reproduces in
`Australia/Lord_Howe`. Previously unreported as far as I can find.

My port deliberately does **not** reproduce it. Propagating a hang into a rewrite
isn't fidelity worth having, and upstream itself returns the correct values when
you start an hour earlier — so the right answers were never in question. It's
registered in a known-divergences file with a deliberately narrow predicate, so it
can never quietly mask a real bug in my own code.

---

**Repo:** https://github.com/sanjaysah101/port-mortem-cron-parser
**Demo:** https://youtu.be/4O2HAY4QAoM

Built for [Port Mortem 2026](https://coderesurrection.com/2026/) by Hackathon
Raptors. Track C, TypeScript → Go.

*Everything above is reproducible: `node run.mjs` clones the pinned upstream,
runs its untouched suite, replays the conformance oracle, and fuzzes.*
