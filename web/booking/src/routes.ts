// Routes of the booking SPA (m4-plan §8.1, m8-plan §12). The SPA has no router: the path picks the page. M4 adds
// the customer magic-link pages under /b/{slug}/m/{purpose}/{token} (quote acceptance, renewal proposal, signing,
// unsubscribe); M5 the pay page; M8 the booking return page /b/{slug}/r/{bookingId} (tokens in the fragment), the
// customer area /b/{slug}/account and its login link /b/{slug}/a (token in the fragment). Everything else is the
// public marina page, which hosts the booking flow (steps as URL hashes).

export const LINK_PURPOSES = ['quote', 'renewal', 'signature', 'unsubscribe'] as const;
export type LinkPurpose = (typeof LINK_PURPOSES)[number];

export type BookingRoute =
  | { page: 'marina'; slug: string | null }
  | { page: 'link'; slug: string; purpose: LinkPurpose; token: string }
  | { page: 'pay'; slug: string; token: string; ret: boolean }
  | { page: 'return'; slug: string; bookingId: string }
  | { page: 'account'; slug: string; contractId: string | null }
  | { page: 'accountLink'; slug: string };

const SLUG = '([a-z0-9-]{3,63})';
const LINK = new RegExp(`^/b/${SLUG}/m/([a-z]+)/([A-Za-z0-9_-]{16,128})/?$`);
// M5 public pay page of a payment request (m5-plan §9.1): /b/{slug}/pay/{token} and its return page.
const PAY = new RegExp(`^/b/${SLUG}/pay/([A-Za-z0-9_-]{16,128})(/return)?/?$`);
// M8 (m8-plan §12): booking return page, customer area (optionally one booking) and login link.
const RETURN = new RegExp(`^/b/${SLUG}/r/([A-Za-z0-9-]{8,64})/?$`);
const ACCOUNT = new RegExp(`^/b/${SLUG}/account(?:/([A-Za-z0-9-]{8,64}))?/?$`);
const ACCOUNT_LINK = new RegExp(`^/b/${SLUG}/a/?$`);

/** Resolves a pathname to a booking page. Unknown link purposes fall back to the marina page. */
export function routeFromPath(
  pathname: string,
  marinaSlug: (p: string) => string | null,
): BookingRoute {
  const pay = PAY.exec(pathname);
  if (pay) return { page: 'pay', slug: pay[1] ?? '', token: pay[2] ?? '', ret: Boolean(pay[3]) };
  const ret = RETURN.exec(pathname);
  if (ret) return { page: 'return', slug: ret[1] ?? '', bookingId: ret[2] ?? '' };
  const account = ACCOUNT.exec(pathname);
  if (account) return { page: 'account', slug: account[1] ?? '', contractId: account[2] ?? null };
  const accountLink = ACCOUNT_LINK.exec(pathname);
  if (accountLink) return { page: 'accountLink', slug: accountLink[1] ?? '' };
  const m = LINK.exec(pathname);
  if (m && (LINK_PURPOSES as readonly string[]).includes(m[2] ?? '')) {
    return { page: 'link', slug: m[1] ?? '', purpose: m[2] as LinkPurpose, token: m[3] ?? '' };
  }
  return { page: 'marina', slug: marinaSlug(pathname) };
}
