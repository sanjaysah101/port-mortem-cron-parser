// Benchmark: cron-parser (TypeScript) on Node.
// Measures parse+iterate latency percentiles, peak RSS, and process startup.
import { CronExpressionParser } from '../upstream/dist/index.js';

const WORKLOAD = [
  ['*/5 * * * *', 'UTC'],
  ['0 0 * * *', 'America/New_York'],
  ['30 1 * * *', 'America/New_York'], // fall-back case
  ['0 2 * * *', 'Australia/Lord_Howe'], // 30-min DST
  ['30 1 * * *', 'Antarctica/Troll'], // 2-hour DST
  ['0 0 L * *', 'UTC'], // last day of month
  ['0 0 * * 5#3', 'Europe/Berlin'], // nth weekday
  ['*/15 9-17 * * 1-5', 'Asia/Kolkata'],
];

const ITERS = parseInt(process.argv[2] || '20000', 10);
const NEXT_PER = 10;

function percentile(sorted, p) {
  const i = Math.min(sorted.length - 1, Math.floor((p / 100) * sorted.length));
  return sorted[i];
}

// Warm up so JIT compilation is not counted.
for (let i = 0; i < 2000; i++) {
  const [expr, tz] = WORKLOAD[i % WORKLOAD.length];
  const it = CronExpressionParser.parse(expr, { tz, currentDate: '2026-06-15T12:00:00' });
  for (let k = 0; k < NEXT_PER; k++) it.next();
}

// Batch the timing to match bench/../port/cmd/bench: Go on Windows has ~1ms
// clock granularity, so the Go side must average over a batch. Node's
// hrtime is nanosecond-resolution and would not need this, but the samples must
// be the same KIND of quantity on both sides for the percentiles to compare.
const BATCH = 200;
const nBatches = Math.max(1, Math.floor(ITERS / BATCH));
const samples = new Float64Array(nBatches);
for (let b = 0; b < nBatches; b++) {
  const t0 = process.hrtime.bigint();
  for (let i = 0; i < BATCH; i++) {
    const [expr, tz] = WORKLOAD[(b * BATCH + i) % WORKLOAD.length];
    const it = CronExpressionParser.parse(expr, { tz, currentDate: '2026-06-15T12:00:00' });
    for (let k = 0; k < NEXT_PER; k++) it.next();
  }
  const t1 = process.hrtime.bigint();
  samples[b] = Number(t1 - t0) / 1000 / BATCH; // us per operation
}

const sorted = Array.from(samples).sort((a, b) => a - b);
const mean = sorted.reduce((a, b) => a + b, 0) / sorted.length;
const mem = process.memoryUsage();

console.log(
  JSON.stringify(
    {
      impl: 'cron-parser (TypeScript)',
      runtime: `node ${process.version}`,
      iterations: ITERS,
        batchSize: BATCH,
        samplesAreBatchMeans: true,
      nextCallsPerIteration: NEXT_PER,
      latency_us: {
        mean: +mean.toFixed(2),
        p50: +percentile(sorted, 50).toFixed(2),
        p90: +percentile(sorted, 90).toFixed(2),
        p99: +percentile(sorted, 99).toFixed(2),
        p999: +percentile(sorted, 99.9).toFixed(2),
        max: +sorted[sorted.length - 1].toFixed(2),
      },
      memory_bytes: {
        rss: mem.rss,
        heapUsed: mem.heapUsed,
        external: mem.external,
      },
    },
    null,
    2,
  ),
);
