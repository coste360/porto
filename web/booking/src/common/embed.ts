// Embed mode of the booking page (m8-plan §10, §16 D18). The iframe URL carries ?embed=1&parent=<origin>
// (embed.js) and the page talks to the parent only when that origin is in the marina's allowlist
// (PublicMarina.embed_origins) AND equals the real embedding origin (location.ancestorOrigins[0] when the browser
// has it, else the origin of document.referrer). Otherwise messaging stays off and the page still works. Messages
// go only to that verified origin, never to '*', and carry no personal data.

export type EmbedParams = { embedded: boolean; parent: string | null };

export function normalizeOrigin(value: string | null | undefined): string | null {
  if (!value) return null;
  try {
    const u = new URL(value);
    if (u.protocol !== 'https:' && u.protocol !== 'http:') return null;
    return u.origin;
  } catch {
    return null;
  }
}

/** Reads ?embed=1&parent=… from a query string. */
export function embedParams(search: string): EmbedParams {
  const q = new URLSearchParams(search);
  const embedded = q.get('embed') === '1';
  return { embedded, parent: embedded ? normalizeOrigin(q.get('parent')) : null };
}

/** The real embedding origin as the browser reports it. */
export function actualParentOrigin(
  doc: Pick<Document, 'referrer'>,
  loc: Pick<Location, 'ancestorOrigins'> | undefined,
): string | null {
  const ancestors = loc?.ancestorOrigins;
  if (ancestors && ancestors.length > 0) return normalizeOrigin(ancestors[0]);
  return normalizeOrigin(doc.referrer);
}

/** The parent origin messages may go to, or null when it is not verified. */
export function verifiedParent(
  claimed: string | null,
  allowed: readonly string[] | undefined,
  actual: string | null,
): string | null {
  if (!claimed || !actual || claimed !== actual) return null;
  const list = (allowed ?? [])
    .map((o) => normalizeOrigin(o))
    .filter((o): o is string => o !== null);
  return list.includes(claimed) ? claimed : null;
}

/** A return_to URL is kept only when its origin is in the allowlist. */
export function allowedReturnTo(
  url: string | null | undefined,
  allowed: readonly string[] | undefined,
): string | null {
  const origin = normalizeOrigin(url);
  if (!origin || !url) return null;
  const list = (allowed ?? []).map((o) => normalizeOrigin(o));
  return list.includes(origin) ? url : null;
}

export type EmbedMessage =
  | { type: 'porto:resize'; height: number }
  | { type: 'porto:step'; step: string }
  | { type: 'porto:booked'; reference: string };

export type Messenger = { origin: string | null; post: (m: EmbedMessage) => void };

/** A messenger posting to target only when origin is verified. */
export function createMessenger(
  origin: string | null,
  target: Pick<Window, 'postMessage'> | null,
): Messenger {
  return {
    origin,
    post: (m) => {
      if (!origin || !target) return;
      target.postMessage(m, origin);
    },
  };
}

/** Reports the page height to the parent whenever it changes; returns the cleanup. */
export function observeHeight(messenger: Messenger, el: HTMLElement): () => void {
  if (!messenger.origin || typeof ResizeObserver === 'undefined') return () => {};
  let last = 0;
  const report = () => {
    const h = Math.ceil(el.scrollHeight);
    if (h !== last) {
      last = h;
      messenger.post({ type: 'porto:resize', height: h });
    }
  };
  const ro = new ResizeObserver(report);
  ro.observe(el);
  report();
  return () => ro.disconnect();
}
