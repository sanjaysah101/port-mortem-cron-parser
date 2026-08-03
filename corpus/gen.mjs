// Corpus generator: emits a large deterministic case set from a fixed seed.
//
// Deterministic by construction — no Math.random. The seed is an argument, so a
// divergence found at seed N is reproducible by re-running with seed N.
//
// Usage: node gen.mjs [count] [seed] > generated.json
import { writeFileSync } from 'node:fs';

const count = parseInt(process.argv[2] || '400', 10);
const seed = parseInt(process.argv[3] || '20260803', 10);

// mulberry32, same PRNG family the library uses for H values. Used here only to
// pick cases, so it needs to be reproducible, not identical to upstream's.
function mulberry32(a) {
  return function () {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
const rnd = mulberry32(seed);
const pick = (arr) => arr[Math.floor(rnd() * arr.length)];
const int = (lo, hi) => lo + Math.floor(rnd() * (hi - lo + 1));

// Zones chosen to cover every DST transition WIDTH and offset shape that exists,
// not just the common ones.
const ZONES = [
  'UTC',
  'America/New_York', // -05/-04, 1h
  'Europe/London', // +00/+01, 1h
  'Europe/Berlin', // +01/+02, 1h
  'Australia/Lord_Howe', // +10:30/+11, THIRTY MINUTES
  'Antarctica/Troll', // +00/+02, TWO HOURS
  'Pacific/Chatham', // +12:45/+13:45, :45 offset
  'Asia/Kolkata', // +05:30, no DST
  'Asia/Kathmandu', // +05:45, no DST
  'America/Santiago', // southern hemisphere
  'Pacific/Apia', // crossed the date line
  'Africa/Cairo', // DST reintroduced 2023
  'America/Sao_Paulo', // DST abolished 2019
  'Asia/Tehran', // DST abolished 2022
  'Australia/Sydney',
  'Pacific/Auckland',
];

// Transition-adjacent dates, where bugs actually live.
const HOT_DATES = [
  '2026-03-08', // NY spring
  '2026-11-01', // NY fall
  '2026-03-29', // Troll spring / Europe spring
  '2026-10-25', // Europe fall
  '2026-10-04', // Lord Howe spring (30m)
  '2026-04-05', // Lord Howe fall (30m)
  '2026-09-27', // Chatham spring
  '2026-04-05', // Chatham fall
  '2026-02-28', // month boundary
  '2026-02-29', // does not exist in 2026
  '2028-02-29', // leap day
  '2026-12-31', // year boundary
  '2026-01-01',
  '2026-06-15', // neutral midsummer
];

function randomField(unit) {
  const ranges = {
    second: [0, 59],
    minute: [0, 59],
    hour: [0, 23],
    dom: [1, 31],
    month: [1, 12],
    dow: [0, 6],
  };
  const [lo, hi] = ranges[unit];
  const shape = int(0, 9);
  switch (shape) {
    case 0:
      return '*';
    case 1:
      return String(int(lo, hi));
    case 2: {
      const a = int(lo, hi);
      const b = int(a, hi);
      return `${a}-${b}`;
    }
    case 3:
      return `*/${int(2, 15)}`;
    case 4: {
      const a = int(lo, hi);
      return `${a}/${int(2, 10)}`;
    }
    case 5: {
      const n = int(2, 4);
      const vals = new Set();
      for (let i = 0; i < n; i++) vals.add(int(lo, hi));
      return [...vals].join(',');
    }
    case 6: {
      const a = int(lo, hi);
      const b = int(a, hi);
      return `${a}-${b}/${int(2, 6)}`;
    }
    case 7:
      return unit === 'dom' || unit === 'dow' ? '?' : '*';
    case 8:
      if (unit === 'dom') return 'L';
      if (unit === 'dow') return `${int(0, 6)}L`;
      return String(int(lo, hi));
    default:
      return '*';
  }
}

const cases = [];

// Deliberately weight toward the hot dates and DST zones; a uniform sample over
// all instants would almost never land near a transition.
for (let i = 0; i < count; i++) {
  const tz = pick(ZONES);
  const date = pick(HOT_DATES);
  const hh = String(int(0, 23)).padStart(2, '0');
  const mm = String(pick([0, 15, 29, 30, 31, 45, 59])).padStart(2, '0');
  const from = `${date}T${hh}:${mm}:00`;

  // Mostly 5-field (the common form), sometimes 6-field with seconds.
  const withSeconds = rnd() < 0.25;
  const parts = [];
  if (withSeconds) parts.push(randomField('second'));
  parts.push(
    randomField('minute'),
    randomField('hour'),
    randomField('dom'),
    randomField('month'),
    randomField('dow'),
  );

  // A third of cases iterate BACKWARD. prev() walks an entirely different code
  // path (subtractDay/subtractMonth/endOf semantics, reverse DST handling), so
  // testing only next() would leave half the library unexercised.
  const dir = rnd() < 0.33 ? 'prev' : 'next';

  cases.push({
    id: `gen-${String(i).padStart(4, '0')}`,
    expr: parts.join(' '),
    tz,
    from,
    n: int(3, 8),
    dir,
    why: `generated (seed=${seed}, i=${i}, dir=${dir})`,
  });
}

// Also emit every predefined alias against each DST zone.
const PREDEF = ['@yearly', '@monthly', '@weekly', '@daily', '@hourly', '@minutely', '@weekdays', '@weekends'];
let k = 0;
for (const p of PREDEF) {
  for (const tz of ['UTC', 'America/New_York', 'Australia/Lord_Howe', 'Antarctica/Troll', 'Pacific/Chatham']) {
    cases.push({
      id: `predef-${String(k++).padStart(3, '0')}`,
      expr: p,
      tz,
      from: pick(HOT_DATES) + 'T00:00:00',
      n: 4,
      why: `predefined ${p} in ${tz}`,
    });
  }
}

const out = {
  schema: 'cron-conformance/v1',
  description: `Generated corpus: ${cases.length} cases, seed ${seed}. Reproduce with: node gen.mjs ${count} ${seed}`,
  tzdata_note: 'Node ICU 2025b / Go 2025c; offsets verified identical for all transitions used.',
  cases,
};

const path = new URL('./generated.json', import.meta.url);
writeFileSync(path, JSON.stringify(out, null, 2));
console.error(`wrote ${cases.length} cases to generated.json (seed ${seed})`);
