/** Where the console lives. Set NEXT_PUBLIC_DASHBOARD_URL when you deploy. */
export const DASHBOARD_URL = (
  process.env.NEXT_PUBLIC_DASHBOARD_URL ?? 'http://localhost:3000'
).replace(/\/$/, '');

/** Public source repository. Set NEXT_PUBLIC_REPO_URL to point a fork at itself. */
export const REPO_URL = (
  process.env.NEXT_PUBLIC_REPO_URL ?? 'https://github.com/kroszborg/bridge'
).replace(/\/$/, '');

export const STATUS_URL = `${DASHBOARD_URL}/status`;
