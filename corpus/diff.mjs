// Differ: compares two conformance emitter outputs case-by-case.
// Usage: node diff.mjs out_ts.json out_go.json
import { readFileSync } from 'node:fs';

const [aPath, bPath] = process.argv.slice(2);
if (!aPath || !bPath) {
  console.error('usage: node diff.mjs <a.json> <b.json>');
  process.exit(2);
}

const A = JSON.parse(readFileSync(aPath, 'utf8'));
const B = JSON.parse(readFileSync(bPath, 'utf8'));
const corpus = JSON.parse(readFileSync(new URL('./' + (process.env.CORPUS || 'cases.json'), import.meta.url), 'utf8'));
const why = new Map(corpus.cases.map((c) => [c.id, c]));

const byId = (r) => new Map(r.results.map((x) => [x.id, x]));
const ma = byId(A);
const mb = byId(B);

const label = (r) => `${r.impl}@${r.version} (${r.lang}, ${r.runtime}, tzdata ${r.tzdata})`;

console.log('CRON CONFORMANCE DIFFERENTIAL');
console.log('='.repeat(78));
console.log(`A = ${label(A)}`);
console.log(`B = ${label(B)}`);
console.log('='.repeat(78));

let agree = 0;
const diverged = [];

for (const c of corpus.cases) {
  const a = ma.get(c.id);
  const b = mb.get(c.id);
  if (!a || !b) {
    console.log(`\n[MISSING] ${c.id}`);
    continue;
  }

  // Compare on the absolute instant (epoch ms) — the thing a scheduler acts on.
  const fa = a.fires.map((f) => f.epoch_ms);
  const fb = b.fires.map((f) => f.epoch_ms);
  const n = Math.min(fa.length, fb.length);
  let firstDiff = -1;
  for (let i = 0; i < n; i++) {
    if (fa[i] !== fb[i]) {
      firstDiff = i;
      break;
    }
  }
  const sameLen = fa.length === fb.length;
  const same = firstDiff === -1 && sameLen && a.error === b.error;

  if (same) {
    agree++;
    console.log(`\n[AGREE ] ${c.id}`);
    continue;
  }

  diverged.push(c.id);
  console.log(`\n[DIVERGE] ${c.id}`);
  console.log(`  expr="${c.expr}"  tz=${c.tz}  from=${c.from}  n=${c.n}`);
  console.log(`  ${c.why}`);
  if (a.error || b.error) {
    console.log(`  A error: ${a.error}`);
    console.log(`  B error: ${b.error}`);
  }
  const rows = Math.max(a.fires.length, b.fires.length);
  console.log(`     ${'A (' + A.lang + ')'.padEnd(28)} | ${'B (' + B.lang + ')'}`);
  for (let i = 0; i < rows; i++) {
    const x = a.fires[i];
    const y = b.fires[i];
    const xs = x ? `${x.local} ${fmtOff(x.offset_min)}` : '—';
    const ys = y ? `${y.local} ${fmtOff(y.offset_min)}` : '—';
    const mark = x && y && x.epoch_ms === y.epoch_ms ? ' ' : '*';
    console.log(`  ${mark}  ${xs.padEnd(30)} | ${ys}`);
  }
}

function fmtOff(min) {
  const s = min < 0 ? '-' : '+';
  const m = Math.abs(min);
  return `${s}${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`;
}

console.log('\n' + '='.repeat(78));
console.log(`SUMMARY: ${agree}/${corpus.cases.length} agree, ${diverged.length} diverge`);
if (diverged.length) {
  console.log('Divergent cases:');
  for (const id of diverged) console.log(`  - ${id}`);
}
console.log('='.repeat(78));
console.log('\nNote: "*" marks a row where the absolute instants differ. Rows are');
console.log('positional, so a single extra/missing fire shifts everything after it.');

process.exit(0);
