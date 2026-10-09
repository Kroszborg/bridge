import { createFromSource } from 'fumadocs-core/search/server';
import { source } from '@/lib/source';

// Written once at build time as out/api/search.json: the docs search index, searched in the browser.
export const dynamic = 'force-static';

export const { staticGET: GET } = createFromSource(source, { language: 'english' });
