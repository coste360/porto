// @ts-check
// Porto documentation site: Astro Starlight with Italian as the root (source)
// locale and English, Pagefind search, the REST API reference generated at
// build time from api/openapi.yaml (starlight-openapi) and every internal link
// checked on build (starlight-links-validator).
//
// Environment (all optional; blank counts as unset, as CI passes repository
// variables that do not exist as empty strings):
//   PORTO_DOCS_SITE     canonical origin, e.g. https://docs.example.org. Not set:
//                       no canonical URLs and no sitemap (the domain of the docs
//                       is not decided yet, so none is hard-coded here)
//   PORTO_DOCS_BASE     base path, e.g. /porto/ for a GitHub Pages project site
//   PORTO_DOCS_VERSION  release the docs describe, shown in the footer
import { satteri } from '@astrojs/markdown-satteri';
import starlight from '@astrojs/starlight';
import { defineConfig } from 'astro/config';
import picomatch from 'picomatch';
import starlightLinksValidator from 'starlight-links-validator';
import starlightOpenAPI, { openAPISidebarGroups } from 'starlight-openapi';
import { baseLinksPlugin } from './src/lib/base-links.mjs';
import { API_REFERENCE_BASE, REPOSITORY_URL } from './src/lib/constants.mjs';

/** @param {string} name */
const env = (name) => process.env[name]?.trim() || undefined;

const site = env('PORTO_DOCS_SITE');
if (site && !URL.canParse(site)) {
  throw new Error(`PORTO_DOCS_SITE must be an absolute URL, got "${site}"`);
}
const base = env('PORTO_DOCS_BASE') ?? '/';
const baseDir = base.endsWith('/') ? base : `${base}/`;
/** URL of a file in public/: absolute when the site origin is known (social cards need it). */
const asset = (/** @type {string} */ file) => (site ? new URL(baseDir + file, site).href : baseDir + file);

const isReferenceLink = picomatch([`${baseDir}${API_REFERENCE_BASE}/**`, `${baseDir}en/${API_REFERENCE_BASE}/**`]);
const homePages = ['/src/content/docs/index.mdx', '/src/content/docs/en/index.mdx'];

export default defineConfig({
  site,
  base,
  trailingSlash: 'ignore',
  markdown: {
    // Root-relative links in the content get the base path (src/lib/base-links.mjs).
    processor: satteri({ hastPlugins: base === '/' ? [] : [baseLinksPlugin(base)] }),
  },
  integrations: [
    starlight({
      title: 'Porto',
      description:
        'Porto, il gestionale gratuito per porti turistici, marine e cantieri nautici di Coste360 SRL: mappa del porto in scala, contratti di ormeggio e transiti, prenotazione online senza commissioni, addebiti e incassi.',
      favicon: '/favicon.svg',
      defaultLocale: 'root',
      locales: {
        root: { label: 'Italiano', lang: 'it' },
        en: { label: 'English', lang: 'en' },
      },
      social: [{ icon: 'github', label: 'GitHub', href: REPOSITORY_URL }],
      editLink: { baseUrl: `${REPOSITORY_URL}/edit/main/docs-site/` },
      lastUpdated: true,
      credits: false,
      customCss: [
        '@fontsource/titillium-web/400.css',
        '@fontsource/titillium-web/600.css',
        '@fontsource/titillium-web/700.css',
        './src/styles/global.css',
      ],
      routeMiddleware: './src/routeData.ts',
      head: [
        { tag: 'meta', attrs: { name: 'theme-color', content: '#0b2542' } },
        { tag: 'meta', attrs: { property: 'og:site_name', content: 'Porto' } },
        { tag: 'meta', attrs: { property: 'og:image', content: asset('og.png') } },
        { tag: 'meta', attrs: { property: 'og:image:width', content: '1200' } },
        { tag: 'meta', attrs: { property: 'og:image:height', content: '630' } },
        { tag: 'meta', attrs: { property: 'og:image:alt', content: 'Porto — Coste360.it' } },
        { tag: 'meta', attrs: { name: 'twitter:card', content: 'summary_large_image' } },
        { tag: 'meta', attrs: { name: 'twitter:image', content: asset('og.png') } },
        { tag: 'link', attrs: { rel: 'icon', type: 'image/png', sizes: '32x32', href: `${baseDir}favicon-32x32.png` } },
        { tag: 'link', attrs: { rel: 'apple-touch-icon', href: `${baseDir}apple-touch-icon.png` } },
      ],
      components: {
        Head: './src/components/overrides/Head.astro',
        // "di Coste360.it" next to the social links: header on wide screens,
        // mobile menu on narrow ones.
        SocialIcons: './src/components/overrides/SocialIcons.astro',
        SiteTitle: './src/components/overrides/SiteTitle.astro',
        Hero: './src/components/overrides/Hero.astro',
        Footer: './src/components/overrides/Footer.astro',
      },
      plugins: [
        starlightOpenAPI([
          {
            base: API_REFERENCE_BASE,
            schema: '../api/openapi.yaml',
            sidebar: {
              label: 'Riferimento API',
              collapsed: true,
              operations: { badges: true, labels: 'summary', sort: 'document' },
            },
            snippets: { operation: { clients: { shell: ['curl'] } } },
          },
        ]),
        starlightLinksValidator({
          // An English page missing its translation falls back to the Italian
          // one, with Starlight's notice: that is not a broken link.
          errorOnFallbackPages: false,
          // Generated routes of the API reference are not content files. The
          // hero actions of the two home pages are relative (`gestore/`) so
          // they work under any base path; every other link is root-relative
          // and checked.
          exclude: ({ file, link }) =>
            isReferenceLink(link) ||
            (homePages.some((home) => file.replaceAll('\\', '/').endsWith(home)) && /^[a-z]/.test(link)),
        }),
      ],
      sidebar: [
        {
          label: 'Guida del gestore',
          translations: { en: 'Operator guide' },
          items: [{ autogenerate: { directory: 'gestore' } }],
        },
        {
          label: 'Installazione e gestione',
          translations: { en: 'Installation and operations' },
          items: [{ autogenerate: { directory: 'installazione' } }],
        },
        {
          label: 'API e webhook',
          translations: { en: 'API and webhooks' },
          items: [{ autogenerate: { directory: 'api' } }, ...openAPISidebarGroups],
        },
        {
          label: 'Sviluppo ed estensioni',
          translations: { en: 'Development and extensions' },
          items: [{ autogenerate: { directory: 'sviluppo' } }],
        },
      ],
    }),
  ],
});
