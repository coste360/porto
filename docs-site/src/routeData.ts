/**
 * Route middleware. The API reference has one generated page per operation
 * (several hundred) and a sidebar group per tag. Rendering all of them in the
 * sidebar of every page would add hundreds of kilobytes of links to each one:
 *
 * - outside the reference, the generated group becomes a single link to the
 *   reference overview;
 * - inside the reference, only the tag group of the current page keeps its
 *   operations; every other tag group becomes a link to its tag overview.
 */
import { defineRouteMiddleware, type StarlightRouteData } from '@astrojs/starlight/route-data';
import { API_REFERENCE_BASE } from './lib/constants.mjs';

type Entry = StarlightRouteData['sidebar'][number];
type Group = Extract<Entry, { type: 'group' }>;

const links = (entry: Entry): Extract<Entry, { type: 'link' }>[] =>
  entry.type === 'link' ? [entry] : entry.entries.flatMap(links);

/** Every link below `entry` points into the reference. */
const onlyReference = (entry: Entry, prefix: string): boolean => {
  const all = links(entry);
  return all.length > 0 && all.every((link) => link.href.startsWith(prefix));
};

const containsCurrent = (entry: Entry): boolean => links(entry).some((link) => link.isCurrent);

const asLink = (group: Group, href: string): Entry => ({
  type: 'link',
  label: group.label,
  href,
  isCurrent: false,
  badge: group.badge,
  attrs: {},
});

/** Outside the reference: the whole reference group becomes one link. */
function collapseReference(entries: Entry[], prefix: string): Entry[] {
  return entries.map((entry): Entry => {
    if (entry.type === 'link') return entry;
    if (onlyReference(entry, prefix)) return asLink(entry, prefix);
    return { ...entry, entries: collapseReference(entry.entries, prefix) };
  });
}

/** Inside the reference: tag groups other than the current one become links. */
function collapseOtherTags(entries: Entry[], prefix: string): Entry[] {
  return entries.map((entry): Entry => {
    if (entry.type === 'link') return entry;
    if (!onlyReference(entry, prefix)) return { ...entry, entries: collapseOtherTags(entry.entries, prefix) };
    const nested = entry.entries.some((child) => child.type === 'group');
    if (nested) return { ...entry, entries: collapseOtherTags(entry.entries, prefix) };
    const first = links(entry)[0];
    return containsCurrent(entry) || !first ? entry : asLink(entry, first.href);
  });
}

export const onRequest = defineRouteMiddleware(async (context, next) => {
  await next();
  const root = import.meta.env.BASE_URL.replace(/\/$/, '');
  const prefix = `${root}/${API_REFERENCE_BASE}/`;
  const route = context.locals.starlightRoute;
  const localized = route.locale ? `${root}/${route.locale}/${API_REFERENCE_BASE}/` : prefix;
  const path = context.url.pathname;
  const inReference = path.startsWith(prefix) || path.startsWith(localized);
  route.sidebar = inReference ? collapseOtherTags(route.sidebar, prefix) : collapseReference(route.sidebar, prefix);
});
