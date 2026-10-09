import { DocsLayout } from 'fumadocs-ui/layouts/docs';
import type { ReactNode } from 'react';
import { Wordmark } from '@/components/brand';
import { DocsProvider } from '@/components/docs/provider';
import { DASHBOARD_URL, REPO_URL } from '@/lib/site';
import { pageTree } from '@/lib/source';

export default function Layout({ children }: { children: ReactNode }) {
  return (
    <DocsProvider>
      <DocsLayout
        tree={pageTree()}
        nav={{
          url: '/',
          title: (
            <span className="flex items-center gap-2">
              <Wordmark />
              <span className="font-mono text-xs font-medium text-fd-muted-foreground">docs</span>
            </span>
          ),
        }}
        githubUrl={REPO_URL}
        links={[
          { text: 'Home', url: '/', active: 'none' },
          { text: 'Dashboard', url: DASHBOARD_URL, external: true },
        ]}
      >
        {children}
      </DocsLayout>
    </DocsProvider>
  );
}
