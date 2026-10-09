import type { MetadataRoute } from 'next';
import { docsUrl } from '@/lib/docs/paths';
import { SITE_URL } from '@/lib/site';
import { source } from '@/lib/source';

// Static export renders metadata routes at build time only when marked static.
export const dynamic = 'force-static';

export default function sitemap(): MetadataRoute.Sitemap {
  const lastModified = new Date();
  return [
    { url: `${SITE_URL}/`, lastModified, changeFrequency: 'weekly', priority: 1 },
    { url: `${SITE_URL}/llms.txt`, lastModified, changeFrequency: 'weekly', priority: 0.5 },
    { url: `${SITE_URL}/privacy/`, lastModified, changeFrequency: 'yearly', priority: 0.3 },
    { url: `${SITE_URL}/terms/`, lastModified, changeFrequency: 'yearly', priority: 0.3 },
    { url: `${SITE_URL}/llms-full.txt`, lastModified, changeFrequency: 'weekly', priority: 0.5 },
    ...source.getPages().map((page) => ({
      // With the trailing slash the static export serves (page.url has none).
      url: `${SITE_URL}${docsUrl(page.slugs)}`,
      lastModified,
      changeFrequency: 'weekly' as const,
      priority: page.slugs.length === 0 ? 0.9 : 0.7,
    })),
  ];
}
