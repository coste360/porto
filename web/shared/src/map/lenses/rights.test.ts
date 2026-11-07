import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

import { daySubjects } from '../dayView';
import { RIGHTS_LEGEND, TOKEN_CLASSES, applyRightsLens, type RightsLegendKey } from './index';
import { DATE, makeDayView } from './testFixtures';

const dv = makeDayView();
const subjects = daySubjects(dv);
const berths = subjects.filter((s) => s.resourceType === 'berth' && s.ref === s.resourceId);

const rights = {
  map_id: 'm1',
  date: DATE,
  items: [
    {
      berth_id: berths[0]!.resourceId,
      right_id: 'r1',
      mandate_id: '',
      status: 'holder' as const,
      unpaid_fees: false,
    },
    {
      berth_id: berths[1]!.resourceId,
      right_id: 'r2',
      mandate_id: 'md1',
      status: 'mandate' as const,
      unpaid_fees: false,
    },
    {
      berth_id: berths[2]!.resourceId,
      right_id: 'r3',
      mandate_id: '',
      status: 'holder' as const,
      unpaid_fees: true,
    },
    {
      berth_id: 'not-on-this-map',
      right_id: 'r9',
      mandate_id: '',
      status: 'holder' as const,
      unpaid_fees: false,
    },
  ],
};

describe("rights lens (Diritti d'uso)", () => {
  it('colours holder, mandate, unpaid and marina berths and counts the legend', () => {
    const r = applyRightsLens(subjects, rights);
    expect(r.styles.get(berths[0]!.ref)).toMatchObject({
      colorToken: 'owner',
      badge: 'D',
      rightsKey: 'holder',
      rightId: 'r1',
    });
    expect(r.styles.get(berths[1]!.ref)).toMatchObject({
      colorToken: 'transit',
      badge: 'M',
      rightsKey: 'mandate',
    });
    expect(r.styles.get(berths[2]!.ref)).toMatchObject({
      colorToken: 'bad',
      glyph: 'alert',
      rightsKey: 'unpaid',
    });
    expect(r.styles.get(berths[3]!.ref)).toMatchObject({ colorToken: 'free', rightsKey: 'marina' });
    const total = Object.values(r.counts).reduce((a, b) => a + b, 0);
    expect(total).toBe(subjects.length);
    expect(r.counts.holder).toBe(1);
    expect(r.counts.mandate).toBe(1);
    expect(r.counts.unpaid).toBe(1);
    expect(r.counts.marina).toBe(berths.length - 3);
    expect(r.counts.none).toBe(subjects.length - berths.length);
  });

  it('treats a missing endpoint answer as all marina and dims hidden keys', () => {
    const r = applyRightsLens(subjects, undefined, new Set(['marina']));
    expect(r.counts.marina).toBe(berths.length);
    expect(r.dimmed.size).toBe(berths.length);
  });

  it('uses only known tokens and has a label for every legend key in it and en', () => {
    const r = applyRightsLens(subjects, rights);
    for (const s of r.styles.values()) expect(TOKEN_CLASSES[s.colorToken]).toBeDefined();
    const dir = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'i18n', 'locales');
    for (const lng of ['it', 'en']) {
      const ns = JSON.parse(readFileSync(join(dir, lng, 'rights.json'), 'utf8')) as {
        lens: { legend: Record<RightsLegendKey, string> };
      };
      for (const k of RIGHTS_LEGEND) expect(ns.lens.legend[k]).toBeTruthy();
    }
  });
});
