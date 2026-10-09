import type { MetadataRoute } from 'next';
import { SITE_URL } from '@/lib/site';

// Static export renders metadata routes at build time only when marked static.
export const dynamic = 'force-static';

/**
 * Search engines and AI search, including assistants fetching a page a person asked about, may
 * crawl, so Bridge can be found and cited. Crawlers that only collect training data may not.
 */
export default function robots(): MetadataRoute.Robots {
  return {
    rules: [
      { userAgent: '*', allow: '/' },
      {
        userAgent: [
          'OAI-SearchBot',
          'ChatGPT-User',
          'Claude-SearchBot',
          'Claude-User',
          'PerplexityBot',
          'Perplexity-User',
        ],
        allow: '/',
      },
      {
        userAgent: [
          'GPTBot',
          'ClaudeBot',
          'anthropic-ai',
          'Google-Extended',
          'Applebot-Extended',
          'CCBot',
          'Bytespider',
          'meta-externalagent',
          'FacebookBot',
          'cohere-ai',
          'cohere-training-data-crawler',
          'Diffbot',
          'omgili',
          'ImagesiftBot',
          'Timpibot',
        ],
        disallow: '/',
      },
    ],
    sitemap: `${SITE_URL}/sitemap.xml`,
    host: SITE_URL,
  };
}
