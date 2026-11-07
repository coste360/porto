// Diritti d'uso lens (spec 14.1, 9.2, m4-plan §8.1): composes the day view with `getMapRights` for the same map
// and night. Every berth is the marina's (no active right), a holder's (active right, owner occupancy) or under a
// management mandate (the marina sells it for the holder); an unpaid fee adds the alert glyph. Land resources and
// area placements carry no right (D15) and stay muted. Pure: no API, no clock.
import type { components } from '../../api/schema';
import type { DaySubject } from '../dayView';
import type { LensStyle } from './types';

type MapRights = components['schemas']['MapRights'];
type MapRightItem = MapRights['items'][number];

/** Legend keys of the lens: `rights:lens.legend.<key>`. */
export const RIGHTS_LEGEND = ['marina', 'holder', 'mandate', 'unpaid', 'none'] as const;
export type RightsLegendKey = (typeof RIGHTS_LEGEND)[number];

export type RightsStyle = LensStyle & { rightsKey: RightsLegendKey; rightId?: string };

/** Index of the endpoint items by berth id. */
export function indexMapRights(r: MapRights | undefined): ReadonlyMap<string, MapRightItem> {
  return new Map((r?.items ?? []).map((i) => [i.berth_id, i]));
}

/** Style of one subject; berths without an item belong to the marina. */
export function rightsStyle(
  s: Pick<DaySubject, 'resourceId' | 'resourceType' | 'ref'>,
  index: ReadonlyMap<string, MapRightItem>,
): RightsStyle {
  if (s.resourceType !== 'berth' || s.ref !== s.resourceId) {
    return { colorToken: 'muted', legendKey: 'none', rightsKey: 'none' };
  }
  const item = index.get(s.resourceId);
  if (!item || item.status === 'marina') {
    return { colorToken: 'free', legendKey: 'free', rightsKey: 'marina' };
  }
  const alert = item.unpaid_fees ? { glyph: 'alert' as const } : {};
  if (item.status === 'mandate') {
    return {
      colorToken: 'transit',
      badge: 'M',
      legendKey: 'transit',
      rightsKey: item.unpaid_fees ? 'unpaid' : 'mandate',
      rightId: item.right_id,
      ...alert,
    };
  }
  return {
    colorToken: item.unpaid_fees ? 'bad' : 'owner',
    badge: 'D',
    legendKey: 'owner',
    rightsKey: item.unpaid_fees ? 'unpaid' : 'holder',
    rightId: item.right_id,
    ...alert,
  };
}

export type RightsLensResult = {
  styles: Map<string, RightsStyle>;
  counts: Record<RightsLegendKey, number>;
  dimmed: Set<string>;
};

/** Styles, legend counts and dimmed refs (hidden legend keys) of every subject of the day view. */
export function applyRightsLens(
  subjects: readonly DaySubject[],
  rights: MapRights | undefined,
  hidden: ReadonlySet<string> = new Set(),
): RightsLensResult {
  const index = indexMapRights(rights);
  const styles = new Map<string, RightsStyle>();
  const counts = Object.fromEntries(RIGHTS_LEGEND.map((k) => [k, 0])) as Record<
    RightsLegendKey,
    number
  >;
  const dimmed = new Set<string>();
  for (const s of subjects) {
    const st = rightsStyle(s, index);
    styles.set(s.ref, st);
    counts[st.rightsKey] += 1;
    if (hidden.has(st.rightsKey)) dimmed.add(s.ref);
  }
  return { styles, counts, dimmed };
}
