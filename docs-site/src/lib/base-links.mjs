// Content pages link to each other with root-relative URLs (`/gestore/…`),
// which is what authors write and what the link validator checks. When the
// site is served under a base path (PORTO_DOCS_BASE, e.g. /porto/ for a
// GitHub Pages project site) those URLs must carry the base: this Markdown
// (hast) plugin adds it to `<a href>` and to the `href` of link components
// in MDX. It runs before the link validator, which then checks the final URL.
import { defineHastPlugin } from 'satteri';

/** Components whose `href` attribute is a page link. */
const LINK_COMPONENTS = new Set(['LinkCard', 'LinkButton', 'a']);

/**
 * @param {string} base the Astro base path, e.g. "/porto/"
 * @returns {(href: string) => string | undefined} the rebased href, or undefined when unchanged
 */
export function makeRebase(base) {
  const prefix = base.replace(/\/+$/, '');
  return (href) => {
    if (!prefix || !href.startsWith('/') || href.startsWith('//')) return undefined;
    if (href === prefix || href.startsWith(`${prefix}/`)) return undefined;
    return `${prefix}${href}`;
  };
}

/** @param {string} base */
export function baseLinksPlugin(base) {
  const rebase = makeRebase(base);
  return () =>
    defineHastPlugin({
      name: 'porto-base-links',
      element: {
        filter: ['a'],
        visit(node, ctx) {
          const href = node.properties?.href;
          const next = typeof href === 'string' ? rebase(href) : undefined;
          if (next) ctx.setProperty(node, 'href', next);
        },
      },
      mdxJsxFlowElement: {
        filter: [],
        visit(node, ctx) {
          if (!node.name || !LINK_COMPONENTS.has(node.name)) return;
          for (const attribute of node.attributes) {
            if (attribute.type !== 'mdxJsxAttribute' || attribute.name !== 'href') continue;
            const next = typeof attribute.value === 'string' ? rebase(attribute.value) : undefined;
            if (next) ctx.setAttribute(node, 'href', next);
          }
        },
      },
    });
}
