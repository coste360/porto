import { describe, expect, it } from 'vitest';

import { daySubjects } from '../dayView';
import {
  SENSORS_LEGEND,
  applySensorsLens,
  sensorEntriesOf,
  sensorEntryStyle,
  sensorsKey,
} from './index';
import { makeDayView } from './testFixtures';

const dv = makeDayView();
const subjects = daySubjects(dv);
const berths = subjects.filter((s) => s.resourceType === 'berth' && s.ref === s.resourceId);

type Entry = Parameters<typeof sensorEntryStyle>[0];
const entry = (over: Partial<Entry>): Entry => ({
  device_id: 'd',
  code: 'S',
  resource_type: 'berth',
  resource_id: berths[0]!.resourceId,
  x_cm: 100,
  y_cm: 200,
  physical: 'free',
  discrepancy: false,
  expectation: 'free',
  health: 'ok',
  open_alerts: [],
  ...over,
});

describe('sensors lens', () => {
  it('a discrepancy wins over health, health over the physical state', () => {
    expect(sensorsKey({ physical: 'occupied', discrepancy: true, health: 'offline' })).toBe(
      'discrepancy',
    );
    expect(sensorsKey({ physical: 'occupied', discrepancy: false, health: 'battery_low' })).toBe(
      'offline',
    );
    expect(sensorsKey({ physical: 'occupied', discrepancy: false, health: 'ok' })).toBe('occupied');
    expect(sensorsKey({ physical: 'free', discrepancy: false })).toBe('free');
    expect(sensorsKey({ physical: 'unknown', discrepancy: false, health: 'ok' })).toBe('offline');
  });

  it('never colour only: occupied and offline carry the antenna, discrepancies the alert glyph', () => {
    expect(sensorEntryStyle(entry({ physical: 'occupied' }))).toMatchObject({
      glyph: 'antenna',
      colorToken: 'sensor-occupied',
    });
    expect(sensorEntryStyle(entry({ discrepancy: true }))).toMatchObject({
      glyph: 'alert',
      colorToken: 'bad',
    });
    expect(sensorEntryStyle(entry({ health: 'silent' }))).toMatchObject({
      glyph: 'antenna',
      badge: '!',
    });
    expect(sensorEntryStyle(entry({})).glyph).toBeUndefined();
  });

  it('colours berths, counts per device, markers for segments and pedestals, dims hidden keys', () => {
    const map = {
      map_id: 'm',
      generated_at: '2027-06-12T08:00:00Z',
      entries: [
        entry({ device_id: 'b0', physical: 'occupied' }),
        entry({
          device_id: 'b1',
          resource_id: berths[1]!.resourceId,
          discrepancy: true,
          physical: 'occupied',
        }),
        entry({
          device_id: 'seg',
          resource_type: 'dynamic_area',
          resource_id: 'area',
          from_cm: 0,
          to_cm: 1000,
          health: 'offline',
        }),
        entry({
          device_id: 'ped',
          resource_type: 'pedestal',
          resource_id: 'ped',
          physical: 'free',
        }),
      ],
    };
    const r = applySensorsLens(subjects, map, new Set(['discrepancy']));
    expect(r.styles.get(berths[0]!.ref)?.sensorsKey).toBe('occupied');
    expect(r.styles.get(berths[1]!.ref)?.sensorsKey).toBe('discrepancy');
    expect(r.dimmed.has(berths[1]!.ref)).toBe(true);
    expect(r.counts.occupied).toBe(1);
    expect(r.counts.discrepancy).toBe(1);
    expect(r.counts.offline).toBe(1);
    expect(r.counts.free).toBe(1);
    expect(r.counts.none).toBe(subjects.length - 2);
    expect(r.markers.map((m) => m.key)).toEqual(['seg', 'ped']);
    expect(r.markers[0]).toMatchObject({
      badge: true,
      glyph: 'antenna',
      ref: 'area',
      x: 100,
      y: 200,
    });
    expect(Object.keys(r.counts)).toEqual([...SENSORS_LEGEND]);
    expect(sensorEntriesOf(map, 'area').map((e) => e.device_id)).toEqual(['seg']);
  });

  it('without a map every subject is muted', () => {
    const r = applySensorsLens(subjects, undefined);
    expect(r.counts.none).toBe(subjects.length);
    expect(r.markers).toEqual([]);
  });
});
