'use client';

import Link from 'next/link';
import { useAuthConfig } from './auth-form';

/** Legal links come from the server (BRIDGE_SITE_URL), so self-hosted installs show their own. */
export function AuthFooter() {
  const { data } = useAuthConfig();
  return (
    <footer className="relative flex flex-wrap items-center justify-center gap-x-5 gap-y-2 px-5 pb-6 text-xs text-muted-foreground">
      {data?.terms_url ? (
        <a href={data.terms_url} className="hover:text-foreground">
          Terms
        </a>
      ) : null}
      {data?.privacy_url ? (
        <a href={data.privacy_url} className="hover:text-foreground">
          Privacy
        </a>
      ) : null}
      <Link href="/status" className="hover:text-foreground">
        Status
      </Link>
      <a href="https://github.com/kroszborg/bridge" className="hover:text-foreground">
        Open source
      </a>
    </footer>
  );
}
