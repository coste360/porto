import { useEffect, useMemo, type ReactNode } from 'react';

import {
  actualParentOrigin,
  createMessenger,
  embedParams,
  observeHeight,
  verifiedParent,
} from './embed';
import { EmbedContext, embedOff, type EmbedState } from './useEmbed';

function computeEmbed(allowed: readonly string[] | undefined): EmbedState {
  if (typeof window === 'undefined') return embedOff;
  const params = embedParams(window.location.search);
  const framed = window.self !== window.top;
  if (!params.embedded || !framed) return embedOff;
  const actual = actualParentOrigin(document, window.location);
  const origin = verifiedParent(params.parent, allowed, actual);
  const referrer = document.referrer || null;
  return {
    embedded: true,
    messenger: createMessenger(origin, origin ? window.parent : null),
    returnTo: origin ? (referrer ?? `${origin}/`) : null,
  };
}

/** Embed mode of the booking page, from the URL and the marina's allowed origins. */
export function EmbedProvider({
  allowed,
  children,
}: {
  allowed: readonly string[] | undefined;
  children: ReactNode;
}) {
  const key = (allowed ?? []).join(' ');
  const value = useMemo(() => computeEmbed(key ? key.split(' ') : []), [key]);
  useEffect(() => observeHeight(value.messenger, document.documentElement), [value]);
  return <EmbedContext.Provider value={value}>{children}</EmbedContext.Provider>;
}

/** Test helper: a fixed embed state. */
export function EmbedTestProvider({ value, children }: { value: EmbedState; children: ReactNode }) {
  return <EmbedContext.Provider value={value}>{children}</EmbedContext.Provider>;
}
