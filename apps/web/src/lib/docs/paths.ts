// Where each Markdown file in the repository is published on the website.
//
// The guides stay in the repository (docs/ and the SDK README) so they read well on GitHub; the
// website renders the same files under /docs. Every mapping between a repository path and a
// /docs URL lives here, so the link rewriter, the page loader and the sitemap agree.
//
// Imported by source.config.ts (bundled by fumadocs-mdx), so it uses relative imports only.

import { repoFile } from '../site';

/** The docs site's base path. */
export const DOCS_BASE = '/docs';

/** Files outside docs/ that are also published as docs pages, with their /docs slugs. */
const EXTRA_PAGES: Record<string, string[]> = {
  'packages/sdk/README.md': ['sdk'],
};

/**
 * The slugs of a published Markdown file, or undefined when the file is not part of the docs.
 *
 * README.md is a folder's index page: `docs/messages/README.md` is `/docs/messages/`.
 */
export function slugsForRepoPath(repoPath: string): string[] | undefined {
  const extra = EXTRA_PAGES[repoPath];
  if (extra) return extra;
  if (!repoPath.startsWith('docs/') || !repoPath.toLowerCase().endsWith('.md')) return undefined;
  const parts = repoPath.slice('docs/'.length, -'.md'.length).split('/');
  if (parts.at(-1)?.toLowerCase() === 'readme') parts.pop();
  return parts;
}

/** The website URL of a set of slugs, with the trailing slash the static export uses. */
export function docsUrl(slugs: string[]): string {
  return slugs.length === 0 ? `${DOCS_BASE}/` : `${DOCS_BASE}/${slugs.join('/')}/`;
}

/** Where a link to a repository file should point: its /docs page, or GitHub. */
export function hrefForRepoPath(repoPath: string, hash = ''): string {
  const slugs = slugsForRepoPath(repoPath);
  const base = slugs ? docsUrl(slugs) : repoFile(repoPath);
  return hash ? `${base}#${hash}` : base;
}
