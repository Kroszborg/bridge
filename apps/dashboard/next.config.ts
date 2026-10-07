import path from 'node:path';
import type { NextConfig } from 'next';

const dev = process.env.NODE_ENV !== 'production';

// The dashboard only talks to its own origin (/api/* is proxied to the Bridge
// API server-side), so the policy can stay tight. Next.js needs inline scripts
// for hydration; 'unsafe-eval' is only added in development for fast refresh.
function policy({ turnstile = false } = {}) {
  // The hosted verification page may load Cloudflare Turnstile.
  const cf = turnstile ? ' https://challenges.cloudflare.com' : '';
  return [
    "default-src 'self'",
    `script-src 'self' 'unsafe-inline'${dev ? " 'unsafe-eval'" : ''}${cf}`,
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data: blob:",
    "font-src 'self'",
    `connect-src 'self'${dev ? ' ws:' : ''}${cf}`,
    `frame-src 'self'${cf}`,
    "frame-ancestors 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "object-src 'none'",
  ].join('; ');
}

const config: NextConfig = {
  output: 'standalone',
  outputFileTracingRoot: path.join(import.meta.dirname, '../../'),
  poweredByHeader: false,
  reactStrictMode: true,
  // Type checking runs with TypeScript 7 via `pnpm typecheck`. Next's built-in
  // checker relies on the TypeScript 5/6 JS API, which TS 7 does not ship.
  typescript: { ignoreBuildErrors: true },
  async headers() {
    return [
      {
        source: '/:path*',
        headers: [
          { key: 'Content-Security-Policy', value: policy() },
          { key: 'X-Content-Type-Options', value: 'nosniff' },
          { key: 'X-Frame-Options', value: 'DENY' },
          { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
          {
            key: 'Permissions-Policy',
            value: 'camera=(), microphone=(), geolocation=(), payment=()',
          },
        ],
      },
      {
        source: '/verify/:publishableKey',
        headers: [{ key: 'Content-Security-Policy', value: policy({ turnstile: true }) }],
      },
      {
        // The drop-in widget is loaded as a module script by other sites,
        // which browsers fetch with CORS.
        source: '/widget.js',
        headers: [
          { key: 'Access-Control-Allow-Origin', value: '*' },
          { key: 'Cross-Origin-Resource-Policy', value: 'cross-origin' },
          { key: 'Cache-Control', value: 'public, max-age=300, must-revalidate' },
        ],
      },
    ];
  },
};

export default config;
