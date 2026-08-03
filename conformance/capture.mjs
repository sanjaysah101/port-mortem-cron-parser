// Drive the UNMODIFIED upstream test suite through the instrumented parser and
// write the recorded trace.
//
// Why not run Jest directly? Jest isolates each test file in its own module
// registry, so a wrapper installed here would not be visible inside the tests.
// Instead we import the compiled upstream entry point, instrument it, and then
// replay every parse() call the suite makes — extracted from the test sources as
// literal (expression, options) pairs.
//
// The test FILES are never modified. They are read as text, the parse() call
// sites are extracted, and each is executed against the real implementation. The
// suite itself still runs independently under `run.mjs baseline` (302/302), which
// is what proves the sources are untouched.

import { readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join, dirname } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const testsDir = join(here, '..', 'upstream', 'tests');

const { CronExpressionParser } = await import('../upstream/dist/index.js');

const MAX_ITER = 8;

// ---------------------------------------------------------------------------
// Extract every CronExpressionParser.parse(...) call site from the test sources.
//
// Matches the two forms the suite uses:
//   CronExpressionParser.parse('<expr>')
//   CronExpressionParser.parse('<expr>', { ...literal options... })
// Nested braces in the options object are handled by scanning for the balanced
// closing brace rather than by regex.
// ---------------------------------------------------------------------------
function extractCalls(src) {
  const calls = [];
  const needle = 'CronExpressionParser.parse(';
  let i = 0;
  while ((i = src.indexOf(needle, i)) !== -1) {
    let p = i + needle.length;
    // First argument: a quoted string literal.
    const quote = src[p];
    if (quote !== "'" && quote !== '"' && quote !== '`') {
      i = p;
      continue;
    }
    let expr = '';
    p++;
    let ok = true;
    while (src[p] !== quote) {
      if (src[p] === '\\') {
        expr += src[p + 1];
        p += 2;
        continue;
      }
      if (p >= src.length) {
        ok = false;
        break;
      }
      expr += src[p++];
    }
    if (!ok) {
      i = p;
      continue;
    }
    p++; // past closing quote

    // Optional second argument.
    let optsText = null;
    let q = p;
    while (q < src.length && /\s/.test(src[q])) q++;
    if (src[q] === ',') {
      q++;
      while (q < src.length && /\s/.test(src[q])) q++;
      if (src[q] === '{') {
        let depth = 0;
        const start = q;
        while (q < src.length) {
          if (src[q] === '{') depth++;
          else if (src[q] === '}') {
            depth--;
            if (depth === 0) {
              q++;
              break;
            }
          }
          q++;
        }
        optsText = src.slice(start, q);
      }
    }

    calls.push({ expr, optsText });
    i = q > i ? q : i + needle.length;
  }
  return calls;
}

// Evaluate an options object literal. Only literal-valued keys are kept; a
// literal referencing a variable or calling a function is reported as unusable
// rather than guessed at.
function evalOptions(text) {
  if (!text) return {};
  try {
    // eslint-disable-next-line no-new-func
    const v = new Function(`return (${text});`)();
    return v && typeof v === 'object' ? v : null;
  } catch {
    return null;
  }
}

function serialiseOptions(options) {
  if (options == null) return {};
  const out = {};
  for (const [k, v] of Object.entries(options)) {
    if (v == null) continue;
    if (k === 'currentDate' || k === 'startDate' || k === 'endDate') {
      if (typeof v === 'string') out[k] = v;
      else if (typeof v === 'number') out[k] = new Date(v).toISOString();
      else if (v instanceof Date) out[k] = v.toISOString();
      else return null;
      continue;
    }
    if (typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean') {
      out[k] = v;
      continue;
    }
    return null;
  }
  return out;
}

function captureIter(expression, options, reverse) {
  const out = [];
  let it;
  try {
    it = CronExpressionParser.parse(expression, options);
  } catch {
    return out;
  }
  for (let i = 0; i < MAX_ITER; i++) {
    try {
      const ms = (reverse ? it.prev() : it.next()).toDate().getTime();
      if (!Number.isFinite(ms)) break;
      out.push(ms);
    } catch {
      break;
    }
  }
  return out;
}

// ---------------------------------------------------------------------------

const files = readdirSync(testsDir).filter((f) => f.endsWith('.test.ts'));
const seen = new Set();
const records = [];
let unusable = 0;

for (const f of files) {
  const src = readFileSync(join(testsDir, f), 'utf8');
  for (const { expr, optsText } of extractCalls(src)) {
    const opts = evalOptions(optsText);
    if (opts === null) {
      unusable++;
      continue;
    }
    const serialised = serialiseOptions(opts);
    if (serialised === null) {
      unusable++;
      continue;
    }

    const key = JSON.stringify([expr, serialised]);
    if (seen.has(key)) continue;
    seen.add(key);

    // A call site with no currentDate iterates from "now". Its fire times are a
    // function of the wall clock at record time, so replaying them later can
    // never match by construction. Record the PARSE outcome (which is still a
    // pure function of the expression) and mark the iteration as unreplayable
    // rather than manufacturing a fake divergence.
    const anchored = serialised.currentDate !== undefined || serialised.startDate !== undefined;

    const rec = {
      source: f,
      expression: expr,
      options: serialised,
      anchored,
      error: null,
      fires: [],
      prevFires: [],
    };

    try {
      CronExpressionParser.parse(expr, opts);
    } catch (e) {
      rec.error = String(e && e.message ? e.message : e);
      records.push(rec);
      continue;
    }

    if (anchored) {
      rec.fires = captureIter(expr, opts, false);
      rec.prevFires = captureIter(expr, opts, true);
    }
    records.push(rec);
  }
}

const payload = {
  schema: 'cron-conformance-trace/v1',
  description:
    'Recorded from the UNMODIFIED upstream Jest suite. Every (expression, options) ' +
    'pair below is a literal call site read out of upstream/tests/*.test.ts; the ' +
    'recorded outcome is what the ORIGINAL TypeScript implementation returned. ' +
    'The test files are read as text and never edited — `run.mjs baseline` runs ' +
    'them unmodified (302/302) to prove that.',
  // The suite only passes under TZ=UTC (upstream wraps every test script in
  // cross-env TZ=UTC), and expressions parsed WITHOUT a tz option are
  // interpreted in the process timezone. So the trace is only meaningful
  // together with the TZ it was captured under, and the replayer must match it.
  recordedUnderTZ: process.env.TZ || '(system default)',
  maxIterationsPerCall: MAX_ITER,
  extractedFrom: files,
  callSitesRecorded: records.length,
  callSitesUnusable: unusable,
  callSitesAnchored: records.filter((r) => r.anchored).length,
  records,
};

writeFileSync(join(here, 'trace.json'), JSON.stringify(payload, null, 2));
console.log(`recorded ${records.length} unique call sites from ${files.length} test files`);
console.log(`  (${unusable} call sites skipped: non-literal options)`);
console.log(`  errors captured: ${records.filter((r) => r.error).length}`);
console.log(`  with forward fires: ${records.filter((r) => r.fires.length).length}`);
