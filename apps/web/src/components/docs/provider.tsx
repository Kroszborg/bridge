'use client';

import { RootProvider } from 'fumadocs-ui/provider/next';
import type { ReactNode } from 'react';
import DocsSearchDialog from '@/components/docs/search';

/** Fumadocs context for /docs. The theme comes from the site's own next-themes provider. */
export function DocsProvider({ children }: { children: ReactNode }) {
  return (
    <RootProvider theme={{ enabled: false }} search={{ SearchDialog: DocsSearchDialog }}>
      {children}
    </RootProvider>
  );
}
