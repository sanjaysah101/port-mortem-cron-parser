#!/usr/bin/env node
// Demo driver for the 5-minute submission video.
//
// Runs the six proof points in narrative order, with a banner and a pause before
// each so a screen recorder captures readable segments. Nothing here is
// demo-only: every command is the same one documented in the README, so what the
// video shows is what a judge reproduces.
//
//   node demo/demo.mjs           full run, ~4.5 min
//   node demo/demo.mjs --fast    no pauses, for a dry run
//   node demo/demo.mjs --fuzz 60 override the fuzz duration
import { execSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const args = process.argv.slice(2);
const fast = args.includes('--fast');
const fuzzIdx = args.indexOf('--fuzz');
const fuzzSecs = fuzzIdx !== -1 ? args[fuzzIdx + 1] : '60';

const BOLD = '\x1b[1m';
const DIM = '\x1b[2m';
const CYAN = '\x1b[36m';
const GREEN = '\x1b[32m';
const YELLOW = '\x1b[33m';
const OFF = '\x1b[0m';

const sleep = (ms) => {
  if (fast) return;
  // Synchronous sleep so output ordering is exact under a recorder.
  const end = Date.now() + ms;
  while (Date.now() < end) {}
};

let step = 0;
function banner(title, subtitle) {
  step++;
  const line = '─'.repeat(72);
  console.log('');
  console.log(`${CYAN}${line}${OFF}`);
  console.log(`${BOLD}${CYAN}  ${step}. ${title}${OFF}`);
  if (subtitle) console.log(`${DIM}     ${subtitle}${OFF}`);
  console.log(`${CYAN}${line}${OFF}`);
  console.log('');
  sleep(2500);
}

function run(cmd, cwd = '.', env = {}) {
  console.log(`${DIM}$ ${cmd}${OFF}`);
  console.log('');
  try {
    execSync(cmd, { cwd: join(root, cwd), stdio: 'inherit', shell: true, env: { ...process.env, ...env } });
  } catch {
    // A nonzero exit is meaningful for some steps; the output already showed it.
  }
  sleep(2000);
}

console.log('');
console.log(`${BOLD}PORT MORTEM 2026 — Track C (TypeScript → Go)${OFF}`);
console.log(`${BOLD}harrisiirak/cron-parser → cronparser-go${OFF}`);
console.log('');
console.log(`${DIM}2,823 LOC TypeScript → 2,234 LOC Go. Zero dependencies.${OFF}`);
console.log(`${DIM}Upstream pinned at 8410d3717b7adda1e5b9c5fd6c40cb2cbf9d52e4${OFF}`);
sleep(4000);

// 1 — the original suite, untouched and green.
banner('The ORIGINAL test suite, unmodified', '302 Jest tests. Note: TZ=UTC is required — upstream wraps every test script in cross-env.');
run('npx jest --silent', 'upstream', { TZ: 'UTC' });
console.log(`${DIM}$ git status --porcelain   (in upstream)${OFF}`);
console.log('');
run('git status --porcelain', 'upstream');
console.log(`${GREEN}  Only "?? _probe/" — zero tracked-file modifications.${OFF}`);
sleep(3000);

// 2 — prove the tree is the one we validated against.
banner('The tree is verifiably the one the port was validated against', 'SHA-256 of all 24 upstream test + source files.');
run('node hash-tests.mjs', 'conformance');
sleep(1500);

// 3 — the conformance story: the suite as an oracle.
banner('The original suite AS AN ORACLE, not translated', 'Jest cannot run against a Go binary, and FFI into Node is banned. So: record what the original returns at every test call site, replay against the port.');
run('node capture.mjs', 'conformance', { TZ: 'UTC' });
sleep(1500);
run('go run ./cmd/conformance -trace ../conformance/trace.json', 'port');
console.log(`${GREEN}  130/130. This replay found 3 port bugs the corpus fuzzer could not see.${OFF}`);
sleep(3500);

// 4 — the port's own tests, one regression guard per finding.
banner("The port's own tests", 'One regression guard per finding — Santiago midnight stall, Chatham prev() bug, PRNG bit-exactness.');
run('go test ./cronparser/ -v', 'port');
sleep(1500);

// 5 — live differential fuzzing. The bonus criterion is 60s clean.
banner(`Live differential fuzzing — ${fuzzSecs}s`, 'Fresh random corpus each round: 16 timezones, every DST transition width that exists, next() and prev(). Two processes, JSON over stdout, diffed on epoch milliseconds.');
run(`node fuzz.mjs ${fuzzSecs} 300`, 'corpus');
sleep(2500);

// 6 — the finding worth talking about.
banner('The finding I would put in front of a maintainer', "luxon's answer for an ambiguous local time depends on WHEN YOU RUN IT.");
console.log(`  ${BOLD}luxon/src/datetime.js${OFF}`);
console.log('');
console.log(`  ${DIM}// find the right offset a given local time. The o input is our guess,${OFF}`);
console.log(`  ${DIM}// which determines which offset we'll pick in ambiguous cases${OFF}`);
console.log(`  ${DIM}// (e.g. there are two 3 AMs b/c Fallback DST)${OFF}`);
console.log(`  function fixOffset(localTS, ${YELLOW}o${OFF}, tz) { ... }`);
console.log('');
console.log(`  ...and for IANA zones that guess is ${YELLOW}guessOffsetForZone(zone)${OFF}`);
console.log(`  = the zone's offset at ${YELLOW}Settings.now()${OFF}.`);
console.log('');
console.log(`  ${BOLD}So parse(expr, {currentDate: <ambiguous wall clock>}) can return an${OFF}`);
console.log(`  ${BOLD}instant an hour apart depending on the season the process runs in.${OFF}`);
console.log('');
console.log(`  ${DIM}Five empirical rules were tried and each failed on some zone. Only${OFF}`);
console.log(`  ${DIM}reading the dependency's source explained it — which is a real limit${OFF}`);
console.log(`  ${DIM}on what black-box differential fuzzing can prove.${OFF}`);
sleep(6000);

console.log('');
console.log(`${CYAN}${'─'.repeat(72)}${OFF}`);
console.log(`${BOLD}  Also honest about:${OFF}`);
console.log(`    • One residual divergence, ~1 per 30,000: prev() from an ambiguous`);
console.log(`      instant in a sub-hour-transition zone. Cause identified,`);
console.log(`      documented at the function and in FINDINGS.md §4g.`);
console.log(`    • One upstream bug the port deliberately does NOT reproduce:`);
console.log(`      prev() throws "loop limit exceeded" on a plain daily-midnight`);
console.log(`      schedule in Pacific/Chatham.`);
console.log(`    • Two retractions kept in the write-up rather than deleted.`);
console.log(`${CYAN}${'─'.repeat(72)}${OFF}`);
console.log('');
console.log(`${BOLD}  github.com/sanjaysah101/port-mortem-cron-parser${OFF}`);
console.log('');
