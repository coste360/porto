// Sensori lens (spec 9.2, 24.5, m12-plan §9.2): composes the day view with `getSensorMap` for the same map. A berth
// device colours its berth: free, physically occupied, discrepancy (physical vs contractual, the server decides)
// or offline (also silent and low battery, with the "!" badge). Segment and pedestal devices are point markers.
// The state is never colour-only: occupied and offline carry the antenna glyph, discrepancies the alert glyph.
// No vessel or customer data reaches the lens. Pure: no API, no clock.
import type { components } from '../../api/schema';
import type { DaySubject } from '../dayView';
import type { MapMarker } from '../elements/Markers';
import type { HarborToken } from './tokens';
import type { LensStyle } from './types';

type SensorMap = components['schemas']['SensorMap'];
type SensorMapEntry = SensorMap['entries'][number];

/** Legend keys of the lens: `sensors:lens.<key>` (`none` = no sensor, muted). */
export const SENSORS_LEGEND = ['free', 'occupied', 'discrepancy', 'offline', 'none'] as const;
export type SensorsLegendKey = (typeof SENSORS_LEGEND)[number];

export const SENSORS_TOKEN: Readonly<Record<SensorsLegendKey, HarborToken>> = {
  free: 'free',
  occupied: 'sensor-occupied',
  discrepancy: 'bad',
  offline: 'muted',
  none: 'muted',
};

export type SensorsStyle = LensStyle & { sensorsKey: SensorsLegendKey; deviceId?: string };

/** Legend key of a device entry: a discrepancy wins over health, health over the physical state. */
export function sensorsKey(
  e: Pick<SensorMapEntry, 'physical' | 'discrepancy' | 'health'>,
): SensorsLegendKey {
  if (e.discrepancy) return 'discrepancy';
  if (e.health && e.health !== 'ok') return 'offline';
  if (e.physical === 'occupied') return 'occupied';
  if (e.physical === 'free') return 'free';
  return 'offline';
}

/** Style of one device entry. */
export function sensorEntryStyle(e: SensorMapEntry): SensorsStyle {
  const k = sensorsKey(e);
  const base = { sensorsKey: k, deviceId: e.device_id, colorToken: SENSORS_TOKEN[k] };
  switch (k) {
    case 'discrepancy':
      return { ...base, glyph: 'alert', legendKey: 'anomaly' };
    case 'offline':
      return { ...base, glyph: 'antenna', badge: '!', legendKey: 'none' };
    case 'occupied':
      return { ...base, glyph: 'antenna', legendKey: 'in' };
    default:
      return { ...base, legendKey: 'free' };
  }
}

export type SensorsLensResult = {
  styles: Map<string, SensorsStyle>;
  counts: Record<SensorsLegendKey, number>;
  dimmed: Set<string>;
  markers: MapMarker[];
};

/**
 * Styles of the berths with a device, markers of segment and pedestal devices, legend counts (one per device;
 * subjects without a device count as `none`) and the dimmed refs of hidden legend keys.
 */
export function applySensorsLens(
  subjects: readonly DaySubject[],
  map: SensorMap | undefined,
  hidden: ReadonlySet<string> = new Set(),
  labelOf: (e: SensorMapEntry) => string = (e) => e.code,
): SensorsLensResult {
  const counts = Object.fromEntries(SENSORS_LEGEND.map((k) => [k, 0])) as Record<
    SensorsLegendKey,
    number
  >;
  const styles = new Map<string, SensorsStyle>();
  const dimmed = new Set<string>();
  const markers: MapMarker[] = [];
  const byBerth = new Map<string, SensorMapEntry>();
  for (const e of map?.entries ?? []) {
    const st = sensorEntryStyle(e);
    counts[st.sensorsKey] += 1;
    if (e.resource_type === 'berth') {
      byBerth.set(e.resource_id, e);
      continue;
    }
    if (hidden.has(st.sensorsKey)) continue;
    markers.push({
      key: e.device_id,
      x: e.x_cm,
      y: e.y_cm,
      glyph: st.glyph ?? 'antenna',
      colorToken: st.colorToken === 'free' ? 'ok' : st.colorToken,
      badge: st.sensorsKey === 'offline',
      label: labelOf(e),
      ref: e.resource_id,
    });
  }
  for (const s of subjects) {
    const e = s.resourceType === 'berth' ? byBerth.get(s.resourceId) : undefined;
    if (!e) {
      styles.set(s.ref, { colorToken: 'muted', legendKey: 'none', sensorsKey: 'none' });
      counts.none += 1;
      if (hidden.has('none')) dimmed.add(s.ref);
      continue;
    }
    const st = sensorEntryStyle(e);
    styles.set(s.ref, st);
    if (hidden.has(st.sensorsKey)) dimmed.add(s.ref);
  }
  return { styles, counts, dimmed, markers };
}

/** The device entries of a resource (quick card). */
export function sensorEntriesOf(map: SensorMap | undefined, resourceId: string): SensorMapEntry[] {
  return (map?.entries ?? []).filter((e) => e.resource_id === resourceId);
}
