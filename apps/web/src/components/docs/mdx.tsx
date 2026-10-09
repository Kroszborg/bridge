import defaultMdxComponents from 'fumadocs-ui/mdx';
import type { MDXComponents } from 'mdx/types';

/** Components for the rendered guides: Fumadocs defaults (code blocks with copy, tables, anchors). */
export function getMDXComponents(components?: MDXComponents) {
  return { ...defaultMdxComponents, ...components } satisfies MDXComponents;
}
