// Fumadocs MDX: the website's /docs pages are the repository's own Markdown guides.
// docs/ is read in place (nothing is copied), plus the TypeScript SDK README.
import path from 'node:path';
import { pageSchema } from 'fumadocs-core/source/schema';
import { defineCollections, defineConfig, defineDocs } from 'fumadocs-mdx/config';
import { z } from 'zod';
import { remarkRepoDocs, repoPathOf, summarize } from './src/lib/docs/markdown';

// `next build` and `next dev` run from apps/web.
const repoRoot = path.resolve(process.cwd(), '../..');

/** Title and description come from each guide's heading and intro, so files need no frontmatter. */
const schema = (ctx: { path: string; source: string }) => {
  const repoPath = repoPathOf(repoRoot, ctx.path);
  const { title, description } = summarize(ctx.source, repoPath);
  return pageSchema.extend({
    title: z.string().default(title),
    description: z.string().default(description),
    /** The file's path in the repository, for "Edit on GitHub". */
    repoPath: z.string().default(repoPath),
  });
};

export const docs = defineDocs({
  dir: '../../docs',
  docs: { schema },
});

export const sdk = defineCollections({
  type: 'doc',
  dir: '../../packages/sdk',
  files: ['README.md'],
  schema,
});

export default defineConfig({
  mdxOptions: {
    remarkPlugins: [[remarkRepoDocs, { repoRoot }]],
    // Fences in a language Shiki does not ship (such as caddyfile) render as plain text.
    rehypeCodeOptions: {
      themes: { light: 'github-light', dark: 'github-dark' },
      defaultColor: false,
      fallbackLanguage: 'text',
    },
  },
});
