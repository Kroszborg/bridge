import 'server-only';
import { cookies, headers } from 'next/headers';

/** Internal URL the dashboard server uses to reach the API. */
export const API_URL = (process.env.BRIDGE_API_URL ?? 'http://localhost:8080').replace(/\/$/, '');

/** URL developers use to reach the API (shown in snippets and docs links). */
export const PUBLIC_API_URL = (process.env.BRIDGE_PUBLIC_API_URL ?? API_URL).replace(/\/$/, '');

export type ServerResult<T> =
  | { ok: true; data: T }
  | { ok: false; status: number; unreachable?: boolean };

/** GET an API path on behalf of the signed-in user (server components only). */
export async function serverGet<T>(path: string): Promise<ServerResult<T>> {
  const [jar, incoming] = await Promise.all([cookies(), headers()]);
  try {
    const res = await fetch(`${API_URL}${path}`, {
      headers: {
        accept: 'application/json',
        cookie: jar.toString(),
        'x-forwarded-for': incoming.get('x-forwarded-for') ?? '',
      },
      cache: 'no-store',
    });
    if (!res.ok) return { ok: false, status: res.status };
    return { ok: true, data: (await res.json()) as T };
  } catch {
    return { ok: false, status: 503, unreachable: true };
  }
}
