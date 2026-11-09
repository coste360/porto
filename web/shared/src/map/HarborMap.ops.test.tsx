import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { renderWithProviders } from '../testing';
import type { VesselItem } from './dayView';
import { HarborMap, type HarborMapMeta } from './HarborMap';
import type { LensStyle } from './lenses/types';
import { buildScene } from './scene';
import { berth } from './testFixtures';
import { fitToView, zoomAt } from './viewport';

const meta: HarborMapMeta = {
  width_cm: 10_000,
  height_cm: 5_000,
  grid_cm: 50,
  water_polygons: [],
  land_polygons: [],
  concession_outline: null,
  background: null,
};

const scene = buildScene([
  { kind: 'berth', el: berth({ key: 'b1', code: 'A-1', anchor: { x: 1000, y: 1000 } }) },
  { kind: 'berth', el: berth({ key: 'b2', code: 'A-2', anchor: { x: 1000, y: 1500 } }) },
]);

const styles = new Map<string, LensStyle>([
  ['b1', { colorToken: 'annual', glyph: 'star', legendKey: 'annual' }],
  ['b2', { colorToken: 'free', legendKey: 'free' }],
]);

const vessel: VesselItem = {
  key: 'al-1',
  version: 1,
  ref: 'b1',
  interactive: false,
  loa: 1000,
  beam: 380,
  hull: 'monohull_sail',
  name: 'Aurora',
  origin: { x: 2000, y: 1000 },
  rotationDd: 1800,
};

const close = zoomAt(fitToView({ minX: 0, minY: 0, maxX: 10_000, maxY: 5_000 }, 800, 400), 8, {
  x: 120,
  y: 120,
});

describe('HarborMap operational props', () => {
  it('fills berths with the lens token, glyph and dimming, and keeps them buttons', () => {
    const { container } = renderWithProviders(
      <HarborMap
        meta={meta}
        scene={scene}
        viewport={close}
        interactive
        labelFor={(i) => (i.kind === 'berth' ? `${i.el.code}, Libero` : '')}
        lensStyles={styles}
        dimmed={new Set(['b2'])}
        vessels={[vessel]}
      />,
    );
    const b1 = container.querySelector('[data-ref="b1"] rect')!;
    expect(b1.getAttribute('class')).toContain('fill-harbor-annual');
    expect(b1.getAttribute('data-lens')).toBe('annual');
    expect(container.querySelector('[data-ref="b2"] rect')!.getAttribute('class')).toContain(
      'fill-harbor-muted',
    );
    expect(container.querySelector('symbol[id$="-glyph-star"]')).not.toBeNull();
    expect(screen.getByRole('button', { name: 'A-1, Libero' })).toBeInTheDocument();
    const v = container.querySelector('[data-vessel="al-1"]')!;
    expect(v.querySelector('path')!.getAttribute('class')).toContain('fill-harbor-annual');
    expect(v.textContent).toBe('Aurora');
  });

  it('draws codes, outlines and glyphs in a colour that contrasts with the fill, glyphs above the boats', () => {
    const { container } = renderWithProviders(
      <HarborMap
        meta={meta}
        scene={scene}
        viewport={close}
        lensStyles={styles}
        vessels={[vessel]}
      />,
    );
    // Solid annual fill: background-coloured code and boat outline; translucent fills keep the foreground.
    const code = container.querySelector('[data-ref="b1"] text');
    if (code) expect(code.getAttribute('class')).toContain('fill-background');
    const hull = container.querySelector('[data-vessel="al-1"] path')!;
    expect(hull.getAttribute('class')).toContain('stroke-background');
    expect(hull.getAttribute('class')).not.toContain('stroke-foreground');
    // The star of b1 is drawn once, in the glyph layer after the vessels, not inside the berth.
    expect(container.querySelector('[data-ref="b1"] use')).toBeNull();
    const layers = [...container.querySelectorAll('[data-layer]')].map((g) =>
      g.getAttribute('data-layer'),
    );
    expect(layers.indexOf('glyphs')).toBeGreaterThan(layers.indexOf('vessels'));
    expect(container.querySelector('[data-layer="glyphs"] [data-glyph="star"] use')).not.toBeNull();
  });

  it('draws no silhouette and no glyph when berths are a few pixels wide', () => {
    const far = fitToView({ minX: 0, minY: 0, maxX: 1_000_000, maxY: 500_000 }, 800, 400);
    const { container } = renderWithProviders(
      <HarborMap meta={meta} scene={scene} viewport={far} lensStyles={styles} vessels={[vessel]} />,
    );
    expect(container.querySelector('[data-layer="vessels"]')).toBeNull();
    expect(container.querySelector('[data-ref="b1"] use')).toBeNull();
    expect(container.querySelector('[data-ref="b1"] rect')).not.toBeNull();
  });

  it('schema mode draws tiles with code and vessel name, no silhouettes', () => {
    const { container } = renderWithProviders(
      <HarborMap
        meta={meta}
        scene={scene}
        viewport={close}
        lensStyles={styles}
        vessels={[vessel]}
        mode="schema"
        tileLabels={new Map([['b1', 'Aurora']])}
      />,
    );
    expect(container.querySelector('[data-layer="vessels"]')).toBeNull();
    expect(container.querySelector('[data-ref="b1"]')!.textContent).toBe('A-1Aurora');
  });

  it('without lens props renders exactly as the M1 viewer', () => {
    const { container } = renderWithProviders(
      <HarborMap meta={meta} scene={scene} viewport={close} />,
    );
    expect(container.querySelector('[data-lens]')).toBeNull();
    expect(container.querySelector('[data-ref="b1"] rect')!.getAttribute('class')).toContain(
      'fill-primary/15',
    );
  });
});
