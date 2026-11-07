// Routes of the booking SPA (m4-plan §8.1). The SPA has no router: the path picks the page. M4 adds the customer
// magic-link pages under /b/{slug}/m/{purpose}/{token} (quote acceptance, renewal proposal, signing,
// unsubscribe); everything else is the public marina page.

export const LINK_PURPOSES = ['quote', 'renewal', 'signature', 'unsubscribe'] as const;
export type LinkPurpose = (typeof LINK_PURPOSES)[number];

export type BookingRoute =
  | { page: 'marina'; slug: string | null }
  | { page: 'link'; slug: string; purpose: LinkPurpose; token: string }
  | { page: 'pay'; slug: string; token: string; ret: boolean };

const LINK = /^\/b\/([a-z0-9-]{3,63})\/m\/([a-z]+)\/([A-Za-z0-9_-]{16,128})\/?$/;
// M5 public pay page of a payment request (m5-plan §9.1): /b/{slug}/pay/{token} and its return page.
const PAY = /^\/b\/([a-z0-9-]{3,63})\/pay\/([A-Za-z0-9_-]{16,128})(\/return)?\/?$/;

/** Resolves a pathname to a booking page. Unknown link purposes fall back to the marina page. */
export function routeFromPath(
  pathname: string,
  marinaSlug: (p: string) => string | null,
): BookingRoute {
  const pay = PAY.exec(pathname);
  if (pay) return { page: 'pay', slug: pay[1] ?? '', token: pay[2] ?? '', ret: Boolean(pay[3]) };
  const m = LINK.exec(pathname);
  if (m && (LINK_PURPOSES as readonly string[]).includes(m[2] ?? '')) {
    return { page: 'link', slug: m[1] ?? '', purpose: m[2] as LinkPurpose, token: m[3] ?? '' };
  }
  return { page: 'marina', slug: marinaSlug(pathname) };
}
