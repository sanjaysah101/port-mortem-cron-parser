# 5-minute demo — recording script

## Setup (once)

```bash
node run.mjs setup      # if upstream/ is not already present
node demo/demo.mjs --fast --fuzz 20   # dry run, ~2 min, confirms everything works
```

Terminal: **maximise it**, font size ~16pt, dark theme. The fuzz output is the
widest thing on screen and needs ~90 columns.

## Record

Start your screen recorder, then:

```bash
node demo/demo.mjs
```

Total runtime ~4.5 min with the built-in pauses. Read the narration below over
each segment — the pauses are sized so you can finish each block before the next
banner appears.

---

## Narration

### Opening (over the title card, ~15s)

> This is a TypeScript-to-Go port of `cron-parser`, a cron expression parser
> downloaded about thirty million times a month. Twenty-eight hundred lines of
> TypeScript, to twenty-two hundred lines of Go with zero dependencies —
> upstream needs luxon, the port needs nothing outside the standard library.
>
> But the port isn't the interesting part. Generating a port is easy. Proving it
> behaves the same is the open problem, so that's what most of this is about.

### 1 — The original suite (~35s)

> First: the original test suite, completely unmodified. Three hundred and two
> tests, all passing.
>
> Note the `TZ=UTC`. That's not me being careful — upstream wraps every one of
> its own test scripts in `cross-env TZ=UTC`, because seventeen of those tests
> encode assumptions about the ambient process timezone. Run bare `npx jest` in
> India and seventeen fail. That detail turns out to matter later.
>
> And `git status` shows only an untracked probe directory. Zero modifications to
> tracked files. The suite the judges hashed is byte-identical.

### 2 — Hashes (~20s)

> To make that checkable rather than just claimed: SHA-256 of all twenty-four
> upstream test and source files, at the pinned commit. I don't vendor upstream
> into the repo — the setup command clones it — so this manifest is how you
> confirm the tree you cloned is the tree the port was validated against. It
> regenerates byte-identical on a fresh clone; I tested that.

### 3 — Conformance: the suite as an oracle (~60s)

> Now the hard question: how do you run three hundred and two *Jest* tests
> against a *Go* binary?
>
> You can't. They're TypeScript calling `expect`. And making Jest drive Go code
> would need FFI into Node, which the rules explicitly ban. Translating the tests
> to Go would give me a suite that no longer matches what was hashed — so a diff
> against the original proves nothing.
>
> So I use the suite as an **oracle** instead. This reads the unmodified test
> files as *text*, extracts all 130 literal `parse` call sites, runs each one
> against the real TypeScript implementation, and records what it returns — the
> exact error string, or the next eight and previous eight fire times as epoch
> milliseconds. Then the Go port replays them and diffs.
>
> One hundred and thirty out of a hundred and thirty. Fifty-two with full
> fire-time comparison; seventy-eight parse-only, because those call sites
> iterate from "now" and can't be replayed by construction — I report them
> separately rather than counting them as passes.
>
> This replay found three port bugs my fuzzer structurally could not, because the
> fuzzer always passed an explicit timezone. The biggest: upstream interprets an
> expression with no timezone option in the *process* zone. I'd defaulted to UTC.
> Ninety-one of these call sites were diverging by exactly five and a half hours.

### 4 — The port's tests (~25s)

> The port's own tests. One regression guard per finding — the Santiago midnight
> stall, the Chatham `prev` bug, and a bit-exactness check on the seeded PRNG,
> including a surrogate-pair case, because upstream's hash function iterates
> UTF-16 code units and Go iterates runes.

### 5 — Live fuzzing (~75s, mostly silent — let it run)

> And this is the differential fuzzer running live. Every round generates a fresh
> random corpus — sixteen timezones covering every DST transition width that
> actually exists: the normal one hour, Lord Howe's *thirty minutes*, Antarctica
> Troll's *two hours*, Chatham's forty-five-minute base offset, zones that
> transition at midnight, zones that abolished DST. Both forward and backward
> iteration.
>
> Two independent processes exchanging JSON — no shared runtime, no FFI. Compared
> on absolute epoch milliseconds, not wall-clock strings, because those two
> disagree exactly at DST transitions, which is the whole subject.
>
> The bonus criterion is sixty seconds with zero divergence. My best run is a
> hundred and eighty seconds, thirty-six thousand cases.

### 6 — The finding (~60s)

> Last thing, and it's the one I'd actually want a maintainer to see.
>
> My fuzzer kept producing divergences of exactly one DST shift on ambiguous
> times — the repeated hour during fall-back. I tried five different rules for
> which of the two valid instants luxon picks. Earliest. Latest. Larger offset.
> Every one of them failed on some zone.
>
> There is no rule. This is luxon's own source. `fixOffset` is a guess-and-correct
> search, and the comment says it outright: the guess *determines which offset
> we'll pick in ambiguous cases*. And for IANA zones that guess is the zone's
> offset at `Settings.now()` — **the current moment**.
>
> So calling `parse` with an ambiguous timestamp can give you an answer an hour
> apart depending on what season the process happens to be running in. That's a
> latent time-bomb for anyone scheduling across a fall-back, and no amount of
> black-box differential testing would have found it, because the behaviour isn't
> a function of the inputs. I had to read the dependency's source.
>
> That's the real lesson here. Differential fuzzing is powerful, and it has a
> ceiling.

### Closing (~25s)

> I'm also honest about what doesn't work. There's one residual divergence at
> about one in thirty thousand — backward iteration from an ambiguous instant in a
> sub-hour zone. I found the cause, tried the faithful fix, it made iteration
> stall instead, and I documented that rather than hiding it.
>
> There's one upstream bug the port deliberately *doesn't* reproduce: `prev` throws
> a loop-limit error on a plain daily-midnight schedule in Chatham. And two
> claims I got wrong along the way are still in the write-up, because being
> corrected by running the code is the entire point.
>
> Everything's on GitHub. Thanks.

---

## If you need to trim to under 5:00

Cut in this order:
1. Section 4 (the port's own tests) — ~25s, least surprising content
2. Shorten the fuzz to `--fuzz 45` and talk over it faster
3. Section 2 (hashes) — fold one sentence into section 1

Do **not** cut section 3 or section 6. Those are the two things no other
submission will have.

## Recording tips

- The fuzz segment scrolls. Let it — the movement reads as "genuinely running"
  better than a static screen does.
- If a fuzz round finds the known divergence, it prints
  `(1 known-divergence)` and keeps going. That's expected and worth saying out
  loud if it happens: it's the Chatham bug, and it's documented.
- Record at 1080p minimum. Terminal text at 720p is unreadable after
  compression.
