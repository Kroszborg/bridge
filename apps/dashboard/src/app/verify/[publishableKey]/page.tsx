import { HostedVerify } from './hosted-verify';

/**
 * Hosted verification page (public, no sign-in):
 *   /verify/{publishableKey}?redirect_uri=<url-encoded>&state=<opaque, optional>
 * On success it returns to redirect_uri with bridge_token and state added;
 * Cancel returns with error=cancelled and state.
 */
export default async function Page({
  params,
  searchParams,
}: {
  params: Promise<{ publishableKey: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const [{ publishableKey }, query] = await Promise.all([params, searchParams]);
  const one = (v: string | string[] | undefined) =>
    Array.isArray(v) ? (v[0] ?? null) : (v ?? null);
  return (
    <HostedVerify
      publishableKey={publishableKey}
      redirectUri={one(query.redirect_uri)}
      state={one(query.state)}
    />
  );
}
