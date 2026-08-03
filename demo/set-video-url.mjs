#!/usr/bin/env node
// Swap the YouTube URL into the README once the video is uploaded.
//
//   node demo/set-video-url.mjs https://youtu.be/XXXXXXXXXXX
//
// Idempotent: run it again with a different URL to update the link.
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join, dirname } from 'node:path';

const url = process.argv[2];
if (!url) {
  console.error('usage: node demo/set-video-url.mjs <youtube-url>');
  process.exit(2);
}
if (!/^https:\/\/(www\.)?(youtube\.com\/watch\?v=|youtu\.be\/)/.test(url)) {
  console.error(`refusing to insert "${url}" — expected a youtube.com/watch?v= or youtu.be/ URL`);
  process.exit(2);
}

const readme = join(dirname(fileURLToPath(import.meta.url)), '..', 'README.md');
const before = readFileSync(readme, 'utf8');

// Match either the placeholder or a previously-inserted URL, so re-running works.
const linkRe = /### ▶ \[Watch the demo video\]\((.*?)\)/;
const m = before.match(linkRe);
if (!m) {
  console.error('could not find the demo link line in README.md — was it edited by hand?');
  process.exit(1);
}

const after = before.replace(linkRe, `### ▶ [Watch the demo video](${url})`);
writeFileSync(readme, after);

console.log(`README.md demo link: ${m[1]} -> ${url}`);
if (m[1] === 'YOUTUBE_URL_HERE') {
  console.log('\nNext:');
  console.log('  git add README.md');
  console.log('  git commit -m "Link the demo video"');
  console.log('  git push');
}
