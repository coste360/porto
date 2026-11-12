// Constants shared by astro.config.mjs and the components (plain JS, so the
// config can import it).

/** Base path of the API reference generated from api/openapi.yaml. */
export const API_REFERENCE_BASE = 'api/riferimento';

/** Public repository of Porto. */
export const REPOSITORY_URL = 'https://github.com/coste360/porto';

/** Website of the maker. */
export const COSTE360_URL = 'https://www.coste360.it/';

/**
 * Matomo on Coste360's own server (site 4, shared with portomarina.app): no
 * cookies, Do Not Track respected.
 */
export const MATOMO = { url: 'https://analytics.coste360.it/', siteId: '4' };

/** Privacy notice of portomarina.app, which covers this site too. */
export const PRIVACY_URL = { it: 'https://portomarina.app/privacy/', en: 'https://portomarina.app/en/privacy/' };
