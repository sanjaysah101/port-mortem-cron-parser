// Hash every upstream test file so judges can verify they are unmodified.
//
// The rules hash the original suite at kickoff and check "whether the tests diff
// from what we hashed". This repo never vendors the test files — `run.mjs setup`
// clones upstream at a pinned commit — so these hashes let anyone confirm the
// tree they cloned is the tree this port was validated against.
import { createHash } from 'node:crypto';
import { readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join, dirname } from 'node:path';
import { execSync } from 'node:child_process';

const here = dirname(fileURLToPath(import.meta.url));
const upstream = join(here, '..', 'upstream');
const testsDir = join(upstream, 'tests');

const sha256 = (buf) => createHash('sha256').update(buf).digest('hex');

const files = readdirSync(testsDir).sort();
const hashes = {};
for (const f of files) {
  hashes[`tests/${f}`] = sha256(readFileSync(join(testsDir, f)));
}

// Also hash the source, so a judge can confirm which implementation the oracle
// was recorded from.
const srcFiles = [];
const walk = (dir, prefix) => {
  for (const e of readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
    if (e.isDirectory()) walk(join(dir, e.name), `${prefix}${e.name}/`);
    else if (e.name.endsWith('.ts')) srcFiles.push(`${prefix}${e.name}`);
  }
};
walk(join(upstream, 'src'), 'src/');
for (const rel of srcFiles) {
  hashes[rel] = sha256(readFileSync(join(upstream, rel)));
}

const commit = execSync('git rev-parse HEAD', { cwd: upstream }).toString().trim();
const dirty = execSync('git status --porcelain', { cwd: upstream })
  .toString()
  .split('\n')
  .filter((l) => l.trim() && !l.startsWith('??'));

const payload = {
  description:
    'SHA-256 of every upstream test and source file, at the pinned commit. ' +
    'Regenerate with `node run.mjs hashes` after `node run.mjs setup`. ' +
    'Any difference means the upstream tree is not the one this port was ' +
    'validated against.',
  upstreamRepo: 'https://github.com/harrisiirak/cron-parser',
  pinnedCommit: commit,
  trackedFileModifications: dirty.length,
  algorithm: 'sha256',
  files: hashes,
};

writeFileSync(join(here, 'upstream-hashes.json'), JSON.stringify(payload, null, 2));

console.log(`hashed ${Object.keys(hashes).length} files at ${commit}`);
console.log(`tracked-file modifications: ${dirty.length}`);
if (dirty.length) {
  console.error('WARNING: upstream tree has modified tracked files:');
  for (const l of dirty) console.error('  ' + l);
  process.exit(1);
}
