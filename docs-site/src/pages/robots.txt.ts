// robots.txt: everything is indexable. The sitemap line is written only when
// the canonical origin is configured (PORTO_DOCS_SITE): without it Starlight
// generates no sitemap.
import type { APIRoute } from 'astro';

export const GET: APIRoute = ({ site }) => {
  const lines = ['User-agent: *', 'Allow: /'];
  if (site) {
    const sitemap = new URL(`${import.meta.env.BASE_URL.replace(/\/?$/, '/')}sitemap-index.xml`, site);
    lines.push('', `Sitemap: ${sitemap.href}`);
  }
  return new Response(`${lines.join('\n')}\n`, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } });
};
