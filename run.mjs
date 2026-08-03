#!/usr/bin/env node
// Portable task runner — same targets as the Makefile, no `make` required.
// Usage: node run.mjs [setup|baseline|probes|diff|all]
import { execSync } from 'node:child_process';
import { existsSync } from 'node:fs';

const PINNED = '8410d3717b7adda1e5b9c5fd6c40cb2cbf9d52e4';

const sh = (cmd, cwd = '.', env = {}) => {
  console.log(`\n$ ${cmd}${cwd !== '.' ? `   (in ${cwd})` : ''}`);
  execSync(cmd, { cwd, stdio: 'inherit', env: { ...process.env, ...env }, shell: true });
};

const targets = {
  setup() {
    if (!existsSync('upstream')) {
      sh('git clone https://github.com/harrisiirak/cron-parser.git upstream');
    }
    sh(`git checkout --quiet ${PINNED}`, 'upstream');
    sh('npm ci --silent', 'upstream');
    sh('npm run build --silent', 'upstream');
    sh('go mod tidy', 'goprobe');
  },

  baseline() {
    // TZ=UTC is required: 17/302 tests encode ambient-timezone assumptions.
    sh('npx jest --silent', 'upstream', { TZ: 'UTC' });
    console.log('\n--- upstream tree (expect ONLY untracked _probe/) ---');
    sh('git status --porcelain', 'upstream');
  },

  probes() {
    for (const p of ['tzfacts', 'repro2', 'verify1b', 'pr435', 'fallback']) {
      sh(`node _probe/${p}.mjs`, 'upstream');
    }
    sh('go run .', 'goprobe');
    sh('go run -tags offsets offsets.go', 'goprobe');
  },

  diff() {
    sh('node emit_ts.mjs > out_ts.json', 'corpus');
    sh('go run -tags emit emit_go.go > ../corpus/out_go.json', 'goprobe');
    sh('node diff.mjs out_ts.json out_go.json', 'corpus');
  },

  /** The port's own Go tests, including one regression guard per finding. */
  test() {
    sh('go build ./...', 'port');
    sh('go test ./cronparser/ -v', 'port');
  },

  /**
   * Record an oracle from the UNMODIFIED upstream tests, then replay it against
   * the port. See CONFORMANCE.md for what this does and does not prove.
   */
  conformance() {
    sh('node capture.mjs', 'conformance', { TZ: 'UTC' });
    sh('go run ./cmd/conformance -trace ../conformance/trace.json', 'port');
  },

  /** SHA-256 of every upstream test file, to show they are unmodified. */
  hashes() {
    sh('node hash-tests.mjs', 'conformance');
  },

  /** Differential: the PORT vs the TypeScript original, on the pinned corpus. */
  port() {
    sh('node emit_ts.mjs > out_ts.json', 'corpus');
    sh('go run ./cmd/emit -corpus ../corpus/cases.json > ../corpus/out_port.json', 'port');
    sh('node diff.mjs out_ts.json out_port.json', 'corpus');
  },

  /** Generated corpus across several seeds. */
  generated() {
    for (const seed of [1, 42, 999, 20260803, 31337]) {
      sh(`node gen.mjs 600 ${seed}`, 'corpus');
      sh('node emit_ts.mjs > gen_ts.json', 'corpus', { CORPUS: 'generated.json' });
      sh('go run ./cmd/emit -corpus ../corpus/generated.json > ../corpus/gen_port.json', 'port');
      sh('node diff.mjs gen_ts.json gen_port.json', 'corpus', { CORPUS: 'generated.json' });
    }
  },

  /** Continuous differential fuzz. Default 180s; the bonus threshold is 60s. */
  fuzz() {
    const secs = process.argv[3] || '180';
    sh(`node fuzz.mjs ${secs} 300`, 'corpus');
  },

  bench() {
    sh('node bench_ts.mjs 20000', 'bench');
    sh('go run ./cmd/bench -iters 20000', 'port');
  },

  all() {
    targets.setup();
    targets.baseline();
    targets.probes();
    targets.diff();
    targets.test();
    targets.port();
    targets.fuzz();
  },
};

const target = process.argv[2] || 'all';
if (!targets[target]) {
  console.error(`unknown target: ${target}\navailable: ${Object.keys(targets).join(', ')}`);
  process.exit(2);
}
targets[target]();
