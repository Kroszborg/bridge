import { DocsBody, DocsPage, DocsTitle, EditOnGitHub } from 'fumadocs-ui/layouts/docs/page';
import type { Metadata } from 'next';
import { notFound } from 'next/navigation';
import { IntegrationsGrid } from '@/components/docs/integrations-grid';
import { getMDXComponents } from '@/components/docs/mdx';
import { docsUrl } from '@/lib/docs/paths';
import { pageMeta, repoFile } from '@/lib/site';
import { source } from '@/lib/source';

type Props = { params: Promise<{ slug?: string[] }> };

export default async function Page({ params }: Props) {
  const { slug } = await params;
  const page = source.getPage(slug);
  if (!page) notFound();

  const MDX = page.data.body;

  return (
    <DocsPage toc={page.data.toc} tableOfContent={{ style: 'clerk' }}>
      <DocsTitle>{page.data.title}</DocsTitle>
      <DocsBody>
        {page.path === 'integrations/index.md' ? <IntegrationsGrid /> : null}
        <MDX components={getMDXComponents()} />
      </DocsBody>
      <EditOnGitHub href={repoFile(page.data.repoPath)} />
    </DocsPage>
  );
}

export function generateStaticParams() {
  return source.generateParams();
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { slug } = await params;
  const page = source.getPage(slug);
  if (!page) notFound();

  const title =
    page.slugs.length === 0 ? 'Bridge documentation' : `${page.data.title} · Bridge docs`;
  return pageMeta({ title, description: page.data.description, path: docsUrl(page.slugs) });
}
