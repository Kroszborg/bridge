import type { NextRequest } from 'next/server';
import { API_URL } from '@/lib/server-api';

/**
 * Same-origin proxy from the browser to the Bridge API. Keeping the API on the
 * dashboard's origin means the session cookie is first-party, no CORS is
 * needed, and the API URL is runtime configuration rather than a build-time constant.
 *
 * The one cross-origin caller is the drop-in widget (/v1/widget/*), embedded on
 * other sites. The API decides which origins it admits, so the Origin header,
 * preflight requests and the API's CORS response headers pass through unchanged.
 */

const FORWARD_REQUEST = [
  'accept',
  'access-control-request-headers',
  'access-control-request-method',
  'content-type',
  'cookie',
  'origin',
  'sec-fetch-site',
  'user-agent',
  'x-request-id',
];
const FORWARD_RESPONSE = [
  'access-control-allow-headers',
  'access-control-allow-methods',
  'access-control-allow-origin',
  'access-control-expose-headers',
  'access-control-max-age',
  'cache-control',
  'content-type',
  'retry-after',
  'vary',
  'x-request-id',
];

async function proxy(req: NextRequest, ctx: { params: Promise<{ path: string[] }> }) {
  const { path } = await ctx.params;
  if (path[0] !== 'v1') {
    return Response.json(
      { error: { code: 'not_found', message: 'Only /api/v1/* is proxied.' } },
      { status: 404 },
    );
  }

  const url = new URL(`${API_URL}/${path.map(encodeURIComponent).join('/')}`);
  url.search = req.nextUrl.search;

  const headers = new Headers();
  for (const name of FORWARD_REQUEST) {
    const v = req.headers.get(name);
    if (v) headers.set(name, v);
  }
  const forwardedFor = req.headers.get('x-forwarded-for');
  if (forwardedFor) headers.set('x-forwarded-for', forwardedFor);

  const hasBody = !['GET', 'HEAD', 'OPTIONS'].includes(req.method);
  let upstream: Response;
  try {
    upstream = await fetch(url, {
      method: req.method,
      headers,
      body: hasBody ? await req.arrayBuffer() : undefined,
      redirect: 'manual',
      cache: 'no-store',
    });
  } catch {
    return Response.json(
      {
        error: {
          code: 'service_unavailable',
          message: 'The dashboard could not reach the Bridge API.',
        },
      },
      { status: 502 },
    );
  }

  const out = new Headers();
  for (const name of FORWARD_RESPONSE) {
    const v = upstream.headers.get(name);
    if (v) out.set(name, v);
  }
  for (const cookie of upstream.headers.getSetCookie()) out.append('set-cookie', cookie);

  return new Response(upstream.status === 204 ? null : upstream.body, {
    status: upstream.status,
    headers: out,
  });
}

export {
  proxy as DELETE,
  proxy as GET,
  proxy as OPTIONS,
  proxy as PATCH,
  proxy as POST,
  proxy as PUT,
};
