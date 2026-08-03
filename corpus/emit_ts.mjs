// Emitter: cron-parser (TypeScript) @ pinned commit 8410d37
// Reads corpus/cases.json, emits results in the shared conformance shape.
//
// Deliberately dependency-free: local wall-clock time and UTC offset are
// derived with Intl.DateTimeFormat rather than luxon, so the emitter does not
// depend on upstream/node_modules and no third-party formatter sits in the
// measurement path.
import { readFileSync } from 'node:fs';
import { CronExpressionParser } from '../upstream/dist/index.js';

const corpus = JSON.parse(readFileSync(new URL('./' + (process.env.CORPUS || 'cases.json'), import.meta.url), 'utf8'));

const pad = (n, w = 2) => String(n).padStart(w, '0');

// Wall-clock fields for an instant in a named zone.
function localParts(date, tz) {
  const fmt = new Intl.DateTimeFormat('en-US', {
    timeZone: tz,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
  const p = {};
  for (const { type, value } of fmt.formatToParts(date)) p[type] = value;
  // Intl renders midnight as hour "24" in some ICU versions; normalize.
  const hour = p.hour === '24' ? '00' : p.hour;
  return `${p.year}-${p.month}-${p.day} ${hour}:${p.minute}:${p.second}`;
}

// UTC offset in minutes = (wall clock read as if UTC) - (true instant).
function offsetMinutes(date, tz) {
  const s = localParts(date, tz);
  const asUTC = Date.UTC(
    +s.slice(0, 4),
    +s.slice(5, 7) - 1,
    +s.slice(8, 10),
    +s.slice(11, 13),
    +s.slice(14, 16),
    +s.slice(17, 19),
  );
  return Math.round((asUTC - date.getTime()) / 60000);
}

const results = [];
for (const c of corpus.cases) {
  const out = { id: c.id, fires: [], error: null };
  // A case may request backward iteration via dir:"prev"; default is forward.
  const reverse = c.dir === 'prev';
  try {
    const it = CronExpressionParser.parse(c.expr, { tz: c.tz, currentDate: c.from });
    for (let i = 0; i < c.n; i++) {
      const d = (reverse ? it.prev() : it.next()).toDate();
      out.fires.push({
        epoch_ms: d.getTime(),
        local: localParts(d, c.tz),
        offset_min: offsetMinutes(d, c.tz),
      });
    }
  } catch (e) {
    out.error = String(e && e.message ? e.message : e);
  }
  results.push(out);
}

const pkg = JSON.parse(readFileSync(new URL('../upstream/package.json', import.meta.url), 'utf8'));

console.log(
  JSON.stringify(
    {
      impl: 'cron-parser',
      lang: 'typescript',
      version: pkg.version,
      runtime: `node ${process.version}`,
      tzdata: process.versions.tz || 'unknown',
      results,
    },
    null,
    2,
  ),
);
