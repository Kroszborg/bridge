// Build-time helpers for the repository's Markdown guides, used by source.config.ts.
//
// The guides have no frontmatter: each starts with a `# Title` and an opening paragraph, which
// read well on GitHub. These helpers derive the page title and description from that, and rewrite
// relative links so they work on the website. Relative imports only (bundled by fumadocs-mdx).

import { existsSync, statSync } from 'node:fs';
import path from 'node:path';
import type { Definition, Link, Root } from 'mdast';
import { visit } from 'unist-util-visit';
import { REPO_URL } from '../site';
import { hrefForRepoPath, slugsForRepoPath } from './paths';

const toPosix = (p: string) => p.split(path.sep).join('/');

/** A file's path relative to the repository root, with forward slashes. */
export function repoPathOf(repoRoot: string, file: string): string {
  return toPosix(path.relative(repoRoot, path.resolve(file)));
}

/** Titles that read better on the website than the file's own heading (keyed by repo path). */
const TITLES: Record<string, string> = {
  'packages/sdk/README.md': 'TypeScript SDK',
};

/** Descriptions for pages whose opening paragraph does not summarise them (keyed by repo path). */
const DESCRIPTIONS: Record<string, string> = {
  'docs/integrations/README.md':
    'Connect Bridge to Supabase, Auth0, Better Auth, Firebase, Clerk, n8n, Zapier and Make: built-in hooks, a few lines of code, or HTTP requests and webhooks.',
  'docs/architecture/era-0-plan.md':
    'How Bridge was planned and built: the original scope, architecture and milestones, kept as a historical record.',
  'docs/messages/README.md':
    'Send an SMS with one API call, follow it from queued to the carrier delivery report, and handle failures, incoming SMS and test mode.',
  'packages/sdk/README.md':
    'The TypeScript SDK for Bridge: send SMS, broadcasts and scheduled messages, verify phone numbers, stream events and verify webhooks. No dependencies.',
  'docs/security/README.md':
    'How Bridge protects API keys, device credentials, webhooks, provider credentials and the data it stores.',
  'docs/integrations/firebase-clerk.md':
    'Use Bridge with Firebase Authentication (through Bridge Verify) and with Clerk (by delivering its SMS from a webhook).',
};

/** Markdown inline syntax reduced to plain text, for titles and meta descriptions. */
function plain(markdown: string): string {
  return markdown
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/(\*\*|__)(.+?)\1/g, '$2')
    .replace(/(\*|_)(.+?)\1/g, '$2')
    .replace(/\s+/g, ' ')
    .trim();
}

/** Shortens text to about `max` characters, preferring a sentence end, then a word end. */
function clip(text: string, max = 180): string {
  if (text.length <= max) return text;
  const head = text.slice(0, max);
  const sentence = head.lastIndexOf('. ');
  if (sentence > max * 0.5) return head.slice(0, sentence + 1);
  const word = head.lastIndexOf(' ');
  return `${head.slice(0, word > 0 ? word : max).replace(/[,;:]$/, '')}…`;
}

/** The title (first `# heading`) and description (first paragraph after it) of a guide. */
export function summarize(
  source: string,
  repoPath: string,
): { title: string; description: string } {
  const lines = source
    .replace(/\r\n/g, '\n')
    .replace(/^---\n[\s\S]*?\n---\n/, '')
    .split('\n');
  let title: string | undefined;
  let description: string | undefined;
  let fence: string | undefined;
  let paragraph: string[] = [];

  for (const line of lines) {
    const trimmed = line.trim();
    const fenceMatch = /^(`{3,}|~{3,})/.exec(trimmed);
    if (fence) {
      if (fenceMatch && trimmed.startsWith(fence)) fence = undefined;
      continue;
    }
    if (fenceMatch) {
      fence = fenceMatch[1];
      paragraph = [];
      continue;
    }
    if (!title) {
      const h1 = /^#\s+(.+?)\s*#*$/.exec(trimmed);
      if (h1?.[1]) title = plain(h1[1]);
      continue;
    }
    // The description comes from the introduction only, never from a later section.
    if (/^#{2,}\s/.test(trimmed)) break;
    if (trimmed === '') {
      if (paragraph.length) break;
      continue;
    }
    // Only prose makes a description: skip tables, lists, quotes, HTML and images.
    if (/^(\||>|[-*+]\s|\d+[.)]\s|<|!\[)/.test(trimmed)) {
      paragraph = [];
      continue;
    }
    paragraph.push(trimmed);
  }
  // An intro that leads into a list ("in three ways:") ends the sentence instead.
  if (paragraph.length) description = clip(plain(paragraph.join(' ')).replace(/:$/, '.'));

  const fallbackTitle = path.posix.basename(path.posix.dirname(repoPath)) || 'Bridge';
  const finalTitle = TITLES[repoPath] ?? title ?? fallbackTitle;
  return {
    title: finalTitle,
    description:
      DESCRIPTIONS[repoPath] ?? description ?? `${finalTitle} in the Bridge documentation.`,
  };
}

/** Rewrites one relative link from a guide at `fileDir` to its website or GitHub URL. */
export function rewriteHref(url: string, fileDir: string, repoRoot: string): string {
  if (!url || /^(?:[a-z][a-z\d+.-]*:|#|\/\/|\/)/i.test(url)) return url;
  const hashAt = url.indexOf('#');
  const target = hashAt === -1 ? url : url.slice(0, hashAt);
  const hash = hashAt === -1 ? '' : url.slice(hashAt + 1);
  if (!target) return url;

  const absolute = path.resolve(fileDir, decodeURI(target));
  const repoPath = repoPathOf(repoRoot, absolute);
  if (repoPath.startsWith('..')) return url;

  if (existsSync(absolute) && statSync(absolute).isDirectory()) {
    const readme = `${repoPath.replace(/\/$/, '')}/README.md`;
    if (slugsForRepoPath(readme) && existsSync(path.join(absolute, 'README.md'))) {
      return hrefForRepoPath(readme, hash);
    }
    return `${REPO_URL}/tree/main/${repoPath}${hash ? `#${hash}` : ''}`;
  }
  // Links to other guides must resolve; other files may be absent from a Docker build context.
  if (slugsForRepoPath(repoPath) && !existsSync(absolute)) {
    console.warn(`[docs] broken link "${url}" in ${repoPathOf(repoRoot, fileDir)}/`);
  }
  return hrefForRepoPath(repoPath, hash);
}

/**
 * Remark plugin for the repository's guides:
 * - drops the leading `# Title` (the page layout renders the title from frontmatter);
 * - points relative links at the matching /docs page, or at GitHub for files outside the docs.
 */
export function remarkRepoDocs(options: { repoRoot: string }) {
  return (tree: Root, file: { path?: string; dirname?: string }) => {
    const first = tree.children.findIndex((node) => node.type === 'heading' && node.depth === 1);
    if (first !== -1) tree.children.splice(first, 1);

    if (!file.path) return;
    const fileDir = path.dirname(file.path);
    visit(tree, (node) => {
      if (node.type === 'link' || node.type === 'definition') {
        const link = node as Link | Definition;
        link.url = rewriteHref(link.url, fileDir, options.repoRoot);
      }
    });
  };
}
