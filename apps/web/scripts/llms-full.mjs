// Builds public/llms-full.txt: the repository README, every guide under docs/ and the SDK README
// in one plain-text file for language models (https://llmstxt.org). Runs before `next build`.
// It never fails the build: missing files are skipped with a warning.

import { existsSync, readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { dirname, join, posix, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const webRoot = resolve(here, '..');
const repoRoot = resolve(webRoot, '../..');
const outFile = join(webRoot, 'public', 'llms-full.txt');
const repoUrl = (process.env.NEXT_PUBLIC_REPO_URL ?? 'https://github.com/kroszborg/bridge').replace(
  /\/$/,
  '',
);

const toPosix = (p) => p.split(sep).join('/');

/** Markdown files under a directory, README.md first in each folder, then alphabetical. */
function markdownUnder(dir) {
  if (!existsSync(dir)) return [];
  const entries = readdirSync(dir).sort((a, b) => {
    if (a === 'README.md') return -1;
    if (b === 'README.md') return 1;
    return a.localeCompare(b);
  });
  const files = [];
  const dirs = [];
  for (const name of entries) {
    const full = join(dir, name);
    const st = statSync(full);
    if (st.isDirectory()) dirs.push(full);
    else if (name.toLowerCase().endsWith('.md')) files.push(full);
  }
  return [...files, ...dirs.flatMap(markdownUnder)];
}

/** Rewrites relative links to absolute GitHub URLs, since the file is read out of context. */
function absolutizeLinks(markdown, file) {
  const fileDir = posix.dirname(file);
  return markdown.replace(/(\]\()([^)\s]+)(\))/g, (match, open, target, close) => {
    if (/^(?:[a-z]+:|#|\/\/)/i.test(target)) return match;
    const [path, hash = ''] = target.split('#');
    if (!path) return match;
    const resolved = posix.normalize(posix.join(fileDir, path));
    if (resolved.startsWith('..')) return match;
    return `${open}${repoUrl}/blob/main/${resolved}${hash ? `#${hash}` : ''}${close}`;
  });
}

function main() {
  const wanted = [
    join(repoRoot, 'README.md'),
    ...markdownUnder(join(repoRoot, 'docs')),
    join(repoRoot, 'packages', 'sdk', 'README.md'),
  ];

  const parts = [
    '# Bridge: full documentation',
    '',
    '> Open-source, self-hosted SMS and phone verification. Send through Android phones you own,',
    '> with delivery reports, signed webhooks, a Verify API for one-time passwords and fallback to',
    '> MSG91, Twilio, Vonage or Plivo. This file concatenates the repository README, every guide',
    '> under docs/ and the TypeScript SDK README. Each section starts with its path in the repository.',
    '',
    `Source: ${repoUrl}`,
    '',
  ];

  let included = 0;
  for (const full of wanted) {
    const rel = toPosix(relative(repoRoot, full));
    if (!existsSync(full)) {
      console.warn(`llms-full: skipping missing ${rel}`);
      continue;
    }
    try {
      const body = readFileSync(full, 'utf8').replace(/\r\n/g, '\n').trim();
      parts.push('---', '', `# ${rel}`, '', `Source: ${repoUrl}/blob/main/${rel}`, '');
      parts.push(absolutizeLinks(body, rel), '');
      included += 1;
    } catch (err) {
      console.warn(`llms-full: could not read ${rel}: ${err instanceof Error ? err.message : err}`);
    }
  }

  writeFileSync(outFile, `${parts.join('\n')}\n`);
  console.log(`llms-full: wrote ${toPosix(relative(webRoot, outFile))} from ${included} files`);
}

try {
  main();
} catch (err) {
  // A missing llms-full.txt must never block a deploy of the website.
  console.warn(`llms-full: not generated: ${err instanceof Error ? err.message : err}`);
}
