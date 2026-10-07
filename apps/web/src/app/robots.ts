import type { MetadataRoute } from 'next';
import { SITE_URL } from '@/lib/site';

// Static export renders metadata routes at build time only when marked static.
export const dynamic = 'force-static';

/** Everyone may crawl, including AI crawlers, so Bridge can be found and cited. */
export default function robots(): MetadataRoute.Robots {
  return {
    rules: [
      { userAgent: '*', allow: '/' },
      {
        userAgent: [
          'GPTBot',
          'OAI-SearchBot',
          'ChatGPT-User',
          'ClaudeBot',
          'Claude-User',
          'Claude-SearchBot',
          'PerplexityBot',
          'Perplexity-User',
          'Google-Extended',
          'Applebot-Extended',
          'CCBot',
        ],
        allow: '/',
      },
    ],
    sitemap: `${SITE_URL}/sitemap.xml`,
    host: SITE_URL,
  };
}
