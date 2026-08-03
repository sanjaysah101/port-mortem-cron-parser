# BENCHMARKS — cron-parser (TypeScript) vs the Go port

Reproduce:

```bash
node bench/bench_ts.mjs 20000              # original
cd port && go run ./cmd/bench -iters 20000 # port
```

## Environment

| | |
| --- | --- |
| OS | Windows 11 Pro 10.0.26200, x86_64 |
| Node | v24.7.0 (V8, JIT) |
| Go | go1.26.3 |
| original | cron-parser 5.7.0 @ `8410d37` (depends on luxon 3.7.2) |
| port | cronparser-go 0.1.0 (stdlib `time` only, no dependencies) |

## Workload

Eight expressions, round-robin, each `Parse()` + 10 × `Next()`. Chosen to
exercise the paths that matter rather than only the fast one:

```
*/5 * * * *          UTC                    trivial
0 0 * * *            America/New_York       common
30 1 * * *           America/New_York       fall-back (repeated hour)
0 2 * * *            Australia/Lord_Howe    30-minute DST transition
30 1 * * *           Antarctica/Troll       2-hour DST transition
0 0 L * *            UTC                    last-day-of-month
0 0 * * 5#3          Europe/Berlin          nth weekday
*/15 9-17 * * 1-5    Asia/Kolkata           business hours, no DST
```

2,000 warmup iterations first, so V8's JIT is hot and Go's allocator and tz
caches are settled. 20,000 measured iterations.

## Methodology note — why samples are batch means

**Go's `time.Now()` on Windows has ~1 ms granularity.** A single
`Parse()` + 10 × `Next()` takes single-digit microseconds, so timing it
individually produced a **p50 of literally 0.00 µs** and a p99 of 1002 µs —
quantization noise, not latency.

Both benchmarks therefore time a **batch of 200 operations** and record the
per-operation mean; percentiles are computed over 100 such batch means. Node's
`hrtime` is nanosecond-resolution and would not need this, but both sides must
report the same *kind* of quantity for the percentiles to be comparable.

Consequence, stated plainly: these percentiles describe **batch-mean spread, not
individual-call tail latency**. A true p99 tail would need a
higher-resolution clock on the Go side (e.g. QPC directly, or running on Linux).
Reporting the un-batched numbers as a 26,000× p99 win would have been
indefensible.

## Latency (µs per Parse + 10×Next)

| metric | TypeScript | Go port | ratio |
| --- | --- | --- | --- |
| mean | 1330.89 | **37.58** | **35.4× faster** |
| p50 | 1326.17 | **37.60** | 35.3× |
| p90 | 1349.84 | **50.99** | 26.5× |
| p99 | 1426.98 | **69.50** | 20.5× |
| p99.9 | 1426.98 | **69.50** | 20.5× |
| max | 1426.98 | **69.50** | 20.5× |

The port is roughly **35× faster** on the mean and **20× faster** at p99. Note
the TypeScript distribution is remarkably flat (p50 1326 → max 1427, a 7.6%
spread), which is what a well-warmed JIT on a uniform workload looks like. The
Go distribution is wider in relative terms (37.6 → 69.5, 85%) because GC pauses
land inside some batches.

Caveat: the original carries luxon, a full date-time library, on every
`CronDate` operation. The port uses `time` from the standard library. A
meaningful share of the gap is that dependency, not the language.

## Memory

| metric | TypeScript | Go port |
| --- | --- | --- |
| RSS / Sys | 88,395,776 B (84.3 MiB) | **17,788,928 B (17.0 MiB)** |
| live heap | 15,169,472 B (14.5 MiB) | **2,859,992 B (2.7 MiB)** |
| total allocated | — | 209,207,872 B (199.5 MiB) |

**5.0× less** memory from the OS, **5.3× smaller** live heap.

Not strictly like-for-like: Node's `rss` is the OS resident set of the whole
process (V8 heap, JIT code, ICU tables); Go's `runtime.MemStats.Sys` is memory
obtained from the OS by the Go runtime and excludes the binary's own text/data
pages. Go's true RSS is somewhat higher than 17.0 MiB. The direction is not in
doubt; the exact multiple is approximate.

## Startup (10 runs each, emitting the 15-case corpus)

| | TypeScript (node) | Go (compiled binary) |
| --- | --- | --- |
| min | 224 ms | **84 ms** |
| median | 237 ms | **94 ms** |
| max | 309 ms | 177 ms |

**2.5× faster** at the median. Includes real work (parse the corpus, run 15
cases, emit JSON), so it is not a pure process-spawn measurement — but both
sides do identical work, so the comparison holds.

Compiled binary: **4,314,112 B (4.1 MiB)**, self-contained, with tzdata embedded
via `import _ "time/tzdata"`. The Node equivalent needs a Node runtime plus
`node_modules` (luxon).

## What this does and does not show

**Does:** the port is substantially faster and lighter on every axis measured,
with the same observable behavior (5,760 differential cases across 9 seeds, plus
90 s / 20,060 cases of continuous fuzzing, zero divergence — see
[FINDINGS.md](FINDINGS.md)).

**Does not:**

- These are **batch means**, not per-call tails. See the methodology note.
- Single-threaded, single-machine, Windows only. No Linux or macOS numbers.
- One workload mix. A pure-`*/5`-in-UTC workload would flatter the port further
  (no tz math); a pathological `L`-with-restricted-month workload would narrow
  the gap, since both implementations then walk day-by-day.
- **Performance was never the goal.** The hackathon scores Behavioral
  Equivalence at 30% and performance at 0%. The port exists to test whether
  equivalence can be *proven*; the speedup is a by-product of dropping luxon for
  the standard library.
