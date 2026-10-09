import { docs, sdk } from 'collections/server';
import { loader, type MetaData, type VirtualFile } from 'fumadocs-core/source';
import { toFumadocsSource } from 'fumadocs-mdx/runtime/server';
import { DOCS_BASE } from '@/lib/docs/paths';

// The sidebar is configured here rather than with meta.json files, so docs/ stays plain Markdown
// that reads the same on GitHub. Keys are folders in the virtual tree ('' is the root).
const FOLDERS: Record<string, MetaData> = {
  '': {
    pages: [
      'index',
      '---Guides---',
      'messages',
      'otp',
      'android',
      'webhooks',
      'integrations',
      'providers',
      'broadcasts',
      'schedules',
      'automation',
      '---Tools---',
      'sdk',
      'cli',
      'mcp',
      '---Operate---',
      'self-hosting',
      'hosted',
      'security',
      'architecture',
      'releasing',
    ],
  },
  android: { title: 'Android app', pages: ['play-store', 'f-droid'] },
  integrations: {
    title: 'Integrations',
    pages: ['supabase', 'auth0', 'better-auth', 'firebase-clerk', 'no-code'],
  },
  'self-hosting': { title: 'Self-hosting', pages: ['lightsail'] },
  hosted: { title: 'Hosted Bridge', pages: ['billing'], defaultOpen: true },
  architecture: { title: 'Architecture', pages: ['future-billing', 'era-0-plan'] },
};

/** Short sidebar labels, keyed by virtual path. The page itself keeps its full title. */
const LABELS: Record<string, string> = {
  'index.md': 'Overview',
  'messages/index.md': 'Messages',
  'otp/index.md': 'Verify (OTP)',
  'android/index.md': 'Android app',
  'android/play-store.md': 'Google Play',
  'android/f-droid.md': 'F-Droid',
  'integrations/index.md': 'Integrations',
  'integrations/supabase.md': 'Supabase',
  'integrations/firebase-clerk.md': 'Firebase and Clerk',
  'integrations/no-code.md': 'n8n, Zapier and Make',
  'providers/index.md': 'SMS providers',
  'schedules/index.md': 'Schedules',
  'automation/index.md': 'Automation',
  'sdk/index.md': 'TypeScript SDK',
  'cli/index.md': 'CLI (bridgectl)',
  'mcp/index.md': 'MCP server',
  'self-hosting/index.md': 'Self-hosting',
  'self-hosting/lightsail.md': 'AWS Lightsail',
  'hosted/billing.md': 'Plans and billing',
  'security/index.md': 'Security model',
  'architecture/future-billing.md': 'Billing design',
  'architecture/era-0-plan.md': 'Design history',
  'releasing.md': 'Releasing',
};

/** README.md is the folder index on GitHub; Fumadocs calls it index.md. */
const asIndex = (path: string) => path.replace(/(^|\/)README\.md$/i, '$1index.md');

function files(): VirtualFile[] {
  const pages = [
    ...docs.toFumadocsSource().files,
    ...toFumadocsSource(sdk, [], { baseDir: 'sdk' }).files,
  ].map((file) => ({ ...file, path: asIndex(file.path) }));
  const metas: VirtualFile[] = Object.entries(FOLDERS).map(([folder, data]) => ({
    type: 'meta',
    path: folder ? `${folder}/meta.json` : 'meta.json',
    data,
  }));
  return [...(pages as VirtualFile[]), ...metas];
}

const all = files();

export const source = loader({
  baseUrl: DOCS_BASE,
  source: { files: all as ReturnType<typeof docs.toFumadocsSource>['files'] },
  plugins: [
    {
      name: 'bridge:labels',
      transformPageTree: {
        file(node, filePath) {
          const label = filePath ? LABELS[filePath] : undefined;
          return label ? { ...node, name: label } : node;
        },
        folder(node, folderPath) {
          const label = LABELS[`${folderPath}/index.md`];
          if (node.index && label) node.index = { ...node.index, name: label };
          return label ? { ...node, name: label } : node;
        },
        // A folder holding only its README is one page: show it as a plain link, not a group.
        root(node) {
          node.children = node.children.map((child) =>
            child.type === 'folder' && child.index && child.children.length === 0
              ? child.index
              : child,
          );
          return node;
        },
      },
    },
  ],
});

export type DocsPage = NonNullable<ReturnType<typeof source.getPage>>;

/**
 * The sidebar tree as plain objects. The loader's nodes can carry symbol-keyed
 * metadata, which React refuses to pass from a Server Component to the docs
 * layout (a Client Component); only string keys are copied, React elements as is.
 */
function plain<T>(value: T): T {
  if (Array.isArray(value)) return value.map(plain) as T;
  if (value && typeof value === 'object' && !('$$typeof' in value)) {
    return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, plain(v)])) as T;
  }
  return value;
}

export function pageTree() {
  return plain(source.getPageTree());
}
