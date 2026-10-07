import type { NextConfig } from 'next';

// A fully static site: `next build` writes plain HTML, CSS and JS to out/,
// which any static host (Cloudflare Pages, Netlify, nginx, S3) can serve.
const config: NextConfig = {
  output: 'export',
  images: { unoptimized: true },
  poweredByHeader: false,
  reactStrictMode: true,
  trailingSlash: true,
  // Type checking runs with TypeScript 7 via `pnpm typecheck` (see the dashboard).
  typescript: { ignoreBuildErrors: true },
};

export default config;
