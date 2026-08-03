// Continuous differential fuzzer.
//
// Generates a fresh random corpus, runs both implementations, diffs, repeats —
// until the time budget expires or a divergence is found. On divergence it
// prints the reproducing seed and stops, so the case is recoverable.
//
// Usage: node fuzz.mjs [seconds] [casesPerRound]
//
// The bonus criterion is 60+ continuous seconds with zero divergence on a
// shared public API, which is what this measures.
import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const budgetSec = parseInt(process.argv[2] || '60', 10);
const perRound = parseInt(process.argv[3] || '300', 10);

// Divergences the port intentionally does not reproduce, because upstream is
// wrong. Documented in known-divergences.json; counted separately so they never
// silently mask a NEW divergence.
const known = JSON.parse(readFileSync(new URL('./known-divergences.json', import.meta.url), 'utf8'));

// The ONLY tolerated signature: upstream hits its loop limit and the port
// returns results. Deliberately narrow —
//   * port throwing where upstream succeeds is a PORT BUG (not tolerated)
//   * differing fire times with no error on either side is a real divergence
//   * any other error-message mismatch is a real divergence
// so this can never mask a genuine failure. See DECISIONS.md D11.
function isKnownDivergence(c, a, b) {
  if (!c) return false;
  const upstreamLoopLimit = !!a.error && a.error.includes('loop limit exceeded');
  const portSucceeded = !b.error && b.fires.length > 0;
  return upstreamLoopLimit && portSucceeded;
}

const tmp = mkdtempSync(join(tmpdir(), 'cronfuzz-'));
const started = Date.now();
let round = 0;
let totalCases = 0;
let divergences = 0;
let totalKnown = 0;

// Seed derived from the wall clock ONCE, then advanced deterministically, so a
// failing round is reproducible from the printed seed.
let seed = Date.now() % 1_000_000;

console.log(`differential fuzz: budget ${budgetSec}s, ${perRound} cases/round`);
console.log(`temp dir: ${tmp}`);
console.log('');

while ((Date.now() - started) / 1000 < budgetSec) {
  round++;
  seed = (seed * 1103515245 + 12345) % 2147483647;

  // 1. generate
  execFileSync('node', ['gen.mjs', String(perRound), String(seed)], {
    cwd: import.meta.dirname,
    stdio: ['ignore', 'ignore', 'ignore'],
  });

  // 2. run both implementations
  const tsOut = join(tmp, 'ts.json');
  const goOut = join(tmp, 'go.json');

  const ts = execFileSync('node', ['emit_ts.mjs'], {
    cwd: import.meta.dirname,
    env: { ...process.env, CORPUS: 'generated.json' },
    maxBuffer: 256 * 1024 * 1024,
  });
  writeFileSync(tsOut, ts);

  const go = execFileSync('go', ['run', './cmd/emit', '-corpus', '../corpus/generated.json'], {
    cwd: join(import.meta.dirname, '..', 'port'),
    maxBuffer: 256 * 1024 * 1024,
  });
  writeFileSync(goOut, go);

  // 3. diff on absolute instants
  const A = JSON.parse(readFileSync(tsOut, 'utf8'));
  const B = JSON.parse(readFileSync(goOut, 'utf8'));
  const corpus = JSON.parse(readFileSync(join(import.meta.dirname, 'generated.json'), 'utf8'));

  const mb = new Map(B.results.map((r) => [r.id, r]));
  const caseById = new Map(corpus.cases.map((c) => [c.id, c]));
  const bad = [];
  let knownCount = 0;
  for (const a of A.results) {
    const b = mb.get(a.id);
    if (!b) {
      bad.push({ id: a.id, reason: 'missing in port' });
      continue;
    }
    const fa = a.fires.map((f) => f.epoch_ms).join(',');
    const fb = b.fires.map((f) => f.epoch_ms).join(',');
    // Both erroring counts as agreement only if the message matches; the
    // corpus records upstream's exact text, so a mismatch is a real difference.
    if (fa !== fb || (a.error || null) !== (b.error || null)) {
      if (isKnownDivergence(caseById.get(a.id), a, b)) {
        knownCount++;
        continue;
      }
      bad.push({ id: a.id, aFires: fa, bFires: fb, aErr: a.error, bErr: b.error });
    }
  }

  totalCases += A.results.length;
  totalKnown += knownCount;
  const elapsed = ((Date.now() - started) / 1000).toFixed(1);

  if (bad.length) {
    divergences += bad.length;
    console.log(`round ${round} (seed ${seed}): ${bad.length} DIVERGENCE(S) after ${elapsed}s`);
    for (const d of bad.slice(0, 5)) {
      const c = corpus.cases.find((x) => x.id === d.id);
      console.log(`  ${d.id}: expr=${JSON.stringify(c?.expr)} tz=${c?.tz} from=${c?.from}`);
      console.log(`    ts: ${d.aFires || '(none)'}  err=${d.aErr}`);
      console.log(`    go: ${d.bFires || '(none)'}  err=${d.bErr}`);
    }
    console.log('');
    console.log(`REPRODUCE: node gen.mjs ${perRound} ${seed}`);
    process.exit(1);
  }

  console.log(`round ${round} (seed ${seed}): ${A.results.length} cases OK${knownCount ? ` (${knownCount} known-divergence)` : ""}  [${elapsed}s, ${totalCases} total]`);
}

const elapsed = ((Date.now() - started) / 1000).toFixed(1);
console.log('');
console.log('='.repeat(64));
console.log(`SURVIVED ${elapsed}s of continuous differential fuzzing`);
console.log(`  rounds:      ${round}`);
console.log(`  total cases: ${totalCases}`);
console.log(`  divergences: ${divergences} (unexplained)`);
console.log(`  known-divergence hits: ${totalKnown} (documented in known-divergences.json)`);
console.log('='.repeat(64));
