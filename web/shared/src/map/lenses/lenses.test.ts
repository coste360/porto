import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

import { daySubjects } from '../dayView';
import {
  DEFAULT_LENS_CONTEXT,
  HARBOR_TOKENS,
  LENSES,
  LENS_IDS,
  LENS_LEGEND,
  TOKEN_CLASSES,
  type LensContext,
  type LensId,
} from './index';
import { DATE, makeDayView } from './testFixtures';

const ctx: LensContext = { date: DATE, ...DEFAULT_LENS_CONTEXT };
const dv = makeDayView();
const subjects = daySubjects(dv);

function styleOf(lens: LensId, ref: string) {
  const s = subjects.find((x) => x.ref === ref)!;
  return LENSES[lens](s.subject, ctx);
}

const LOCALES = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'i18n', 'locales');

describe('lenses', () => {
  it('occupancy colours each night status with its glyph', () => {
    expect(styleOf('occupancy', 'a1')).toEqual({ colorToken: 'free', legendKey: 'free' });
    expect(styleOf('occupancy', 'a2')).toEqual({
      colorToken: 'annual',
      glyph: 'star',
      legendKey: 'annual',
    });
    expect(styleOf('occupancy', 'a3')).toMatchObject({ glyph: 'suitcase', legendKey: 'transit' });
    expect(styleOf('occupancy', 'a4')).toMatchObject({ glyph: 'clock', legendKey: 'hold' });
    expect(styleOf('occupancy', 'a5')).toMatchObject({ glyph: 'release', legendKey: 'released' });
    expect(styleOf('occupancy', 'a6')).toMatchObject({ glyph: 'lock', legendKey: 'blocked' });
    expect(styleOf('occupancy', 'a7')).toMatchObject({ glyph: 'wrench', legendKey: 'maintenance' });
    expect(styleOf('occupancy', 'a8')).toMatchObject({ legendKey: 'closed' });
    expect(styleOf('occupancy', 'alloc:al-9')).toMatchObject({ legendKey: 'transit' });
  });

  it('contract type uses the term and the initial of the type', () => {
    expect(styleOf('contractType', 'a2')).toMatchObject({ badge: 'A', legendKey: 'annual' });
    expect(styleOf('contractType', 'a3')).toMatchObject({ badge: 'T', legendKey: 'transit' });
    expect(styleOf('contractType', 'a5')).toMatchObject({ badge: 'S', legendKey: 'seasonal' });
    expect(styleOf('contractType', 'a1')).toMatchObject({ legendKey: 'none' });
  });

  it('vessel type follows the hull', () => {
    expect(styleOf('vesselType', 'a3')).toMatchObject({ legendKey: 'catamaran' });
    expect(styleOf('vesselType', 'a5')).toMatchObject({ legendKey: 'monohull_motor' });
    expect(styleOf('vesselType', 'alloc:al-9')).toMatchObject({ legendKey: 'rib' });
    expect(styleOf('vesselType', 'a1')).toMatchObject({ legendKey: 'none' });
  });

  it('presence tells in port, arrivals and long absences', () => {
    expect(styleOf('presence', 'a2')).toMatchObject({ glyph: 'arrowIn', legendKey: 'in' });
    expect(styleOf('presence', 'a3')).toMatchObject({ legendKey: 'arrival' });
    expect(styleOf('presence', 'a5')).toMatchObject({
      colorToken: 'away-long',
      badge: '37',
      legendKey: 'awayLong',
    });
    expect(
      LENSES.presence(subjects.find((s) => s.ref === 'a5')!.subject, {
        ...ctx,
        absentDaysThreshold: 60,
      }),
    ).toMatchObject({ legendKey: 'out' });
    expect(styleOf('presence', 'a1')).toMatchObject({ legendKey: 'free' });
  });

  it('deadlines grade insurance, documents and the contract end', () => {
    expect(styleOf('deadlines', 'a2')).toMatchObject({ legendKey: 'ok' });
    expect(styleOf('deadlines', 'a3')).toMatchObject({ glyph: 'calendar', legendKey: 'bad' });
    expect(styleOf('deadlines', 'a5')).toMatchObject({ badge: '13', legendKey: 'warn' });
  });

  it('fitness uses the 85 % and 60 % thresholds and the override', () => {
    expect(styleOf('fitness', 'a2')).toMatchObject({ badge: '88%', legendKey: 'ok' });
    expect(styleOf('fitness', 'a3')).toMatchObject({ badge: '50%', legendKey: 'bad' });
    expect(styleOf('fitness', 'a5')).toMatchObject({
      colorToken: 'override',
      legendKey: 'override',
    });
  });

  it('utilities and anomalies', () => {
    expect(styleOf('utilities', 'a2')).toMatchObject({
      glyph: 'bolt',
      badge: '32A',
      legendKey: 'power',
    });
    expect(styleOf('utilities', 'a1')).toMatchObject({ legendKey: 'none' });
    expect(styleOf('anomalies', 'a5')).toMatchObject({
      glyph: 'alert',
      badge: '2',
      legendKey: 'anomaly',
    });
    expect(styleOf('anomalies', 'a2')).toMatchObject({ legendKey: 'none' });
  });

  it('every lens returns a legend key of its legend and a known token for every subject', () => {
    for (const lens of LENS_IDS) {
      for (const s of subjects) {
        const style = LENSES[lens](s.subject, ctx);
        expect(LENS_LEGEND[lens]).toContain(style.legendKey);
        expect(HARBOR_TOKENS).toContain(style.colorToken);
      }
    }
  });

  it('has an i18n label for every lens and legend key, and classes for every token', () => {
    for (const lng of ['it', 'en']) {
      const json = JSON.parse(readFileSync(join(LOCALES, lng, 'occupancyEnums.json'), 'utf8')) as {
        lens: Record<string, unknown> & { legend: Record<string, string> };
      };
      for (const lens of LENS_IDS) {
        expect(json.lens[lens], `${lng} lens.${lens}`).toBeTypeOf('string');
        for (const key of LENS_LEGEND[lens]) {
          expect(json.lens.legend[key], `${lng} lens.legend.${key}`).toBeTypeOf('string');
        }
      }
    }
    for (const token of HARBOR_TOKENS) {
      expect(TOKEN_CLASSES[token].fill).toMatch(/^fill-harbor-/);
      expect(TOKEN_CLASSES[token].bg).toMatch(/^bg-harbor-/);
    }
  });
});
