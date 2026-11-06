import { fireEvent, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import type { Decoration } from '../api/schema';
import { renderWithProviders } from '../testing';
import { HarborMap, type HarborMapMeta } from './HarborMap';
import { LABEL_MIN_PX, STRUCTURE_LABEL_PX } from './elements/common';
import { structureLabelPlacement } from './elements/Structure';
import { DEFAULT_LAYERS } from './layers';
import { buildScene, type SceneEntry } from './scene';
import { berth } from './testFixtures';
import { fitToView, pxPerCm, zoomAt } from './viewport';

const meta: HarborMapMeta = {
  width_cm: 100_000,
  height_cm: 50_000,
  grid_cm: 50,
  water_polygons: [],
  land_polygons: [],
  concession_outline: null,
  background: null,
};

function berths(n: number): SceneEntry[] {
  return Array.from({ length: n }, (_, i) => ({
    kind: 'berth' as const,
    el: berth({
      key: `b${i}`,
      code: `B-${i}`,
      anchor: { x: 500 + (i % 50) * 500, y: 500 + Math.floor(i / 50) * 1500 },
    }),
  }));
}

describe('HarborMap', () => {
  it('culls elements outside the view and exposes interactive elements as buttons', () => {
    const scene = buildScene(berths(200));
    const vp = zoomAt(
      fitToView(meta && { minX: 0, minY: 0, maxX: 100_000, maxY: 50_000 }, 800, 400),
      20,
      { x: 0, y: 0 },
    );
    const onDown = vi.fn();
    renderWithProviders(
      <HarborMap
        meta={meta}
        scene={scene}
        viewport={vp}
        interactive
        labelFor={(item) => `Posto ${item.kind === 'berth' ? item.el.code : ''}`}
        onElementPointerDown={onDown}
      />,
    );
    const buttons = screen.getAllByRole('button');
    expect(buttons.length).toBeGreaterThan(0);
    expect(buttons.length).toBeLessThan(200);
    const first = screen.getByRole('button', { name: 'Posto B-0' });
    fireEvent.pointerDown(first);
    expect(onDown).toHaveBeenCalledWith('b0', expect.anything());
  });

  it('switches to the simple level of detail when zoomed out and hides labels', () => {
    const scene = buildScene(berths(10));
    const vp = fitToView({ minX: 0, minY: 0, maxX: 1_000_000, maxY: 1_000_000 }, 400, 400);
    const { container } = renderWithProviders(
      <HarborMap meta={meta} scene={scene} viewport={vp} />,
    );
    expect(container.querySelectorAll('[data-ref]')).toHaveLength(10);
    expect(container.querySelectorAll('[data-ref] text')).toHaveLength(0);
  });

  it('draws labels when large enough and respects layers', () => {
    const scene = buildScene(berths(3));
    const vp = fitToView({ minX: 0, minY: 0, maxX: 3000, maxY: 2000 }, 800, 600);
    const { container, rerender } = renderWithProviders(
      <HarborMap meta={meta} scene={scene} viewport={vp} />,
    );
    expect(screen.getByText('B-0')).toBeInTheDocument();
    rerender(
      <HarborMap
        meta={meta}
        scene={scene}
        viewport={vp}
        layers={{ ...DEFAULT_LAYERS, berths: false }}
      />,
    );
    expect(container.querySelectorAll('[data-ref]')).toHaveLength(0);
  });

  it('emits kit symbols only for used assets, without innerHTML', () => {
    const deco: Decoration = {
      key: 'd',
      kind: 'symbol',
      asset: 'fuel_station',
      position: { x: 100, y: 100 },
      size_cm: 500,
    };
    const scene = buildScene([{ kind: 'decoration', el: deco }]);
    const vp = fitToView({ minX: 0, minY: 0, maxX: 2000, maxY: 2000 }, 400, 400);
    const { container } = renderWithProviders(
      <HarborMap meta={meta} scene={scene} viewport={vp} />,
    );
    const symbols = container.querySelectorAll('symbol');
    expect(symbols).toHaveLength(1);
    expect(symbols[0]?.id).toMatch(/-kit-fuel_station$/);
    expect(container.querySelector('use')?.getAttribute('href')).toBe(`#${symbols[0]?.id}`);
  });

  it('keeps structure names readable at fit to view and shows berth numbers only from LABEL_MIN_PX', () => {
    const pier: SceneEntry = {
      kind: 'structure',
      el: {
        key: 'p',
        kind: 'pier',
        code: 'A',
        points: [
          { x: 1000, y: 0 },
          { x: 1000, y: 10_000 },
        ],
        width_cm: 300,
      },
    };
    const scene = buildScene([pier, ...berths(3)]);
    // A 600 m harbour on 800 px: about 0.012 px/cm, a 4 m berth label would be about 2 px.
    const far = fitToView({ minX: 0, minY: 0, maxX: 60_000, maxY: 40_000 }, 800, 600);
    const { container, rerender } = renderWithProviders(
      <HarborMap meta={meta} scene={scene} viewport={far} />,
    );
    const label = screen.getByTestId('structure-label');
    expect(label).toHaveTextContent('A');
    const fontPx = Number(label.getAttribute('font-size')) * pxPerCm(far);
    expect(fontPx).toBeGreaterThanOrEqual(STRUCTURE_LABEL_PX - 0.01);
    expect(screen.queryByText('B-0')).toBeNull();

    // Berth numbers appear once their font is at least LABEL_MIN_PX on screen (400 cm wide berth: 168 cm font).
    const sNeeded = LABEL_MIN_PX / 168;
    const near = fitToView(
      { minX: 0, minY: 0, maxX: 700 / sNeeded, maxY: 500 / sNeeded },
      800,
      600,
      0,
    );
    rerender(<HarborMap meta={meta} scene={scene} viewport={near} />);
    expect(screen.getByText('B-0')).toBeInTheDocument();
    for (const t of container.querySelectorAll('[data-kind="berth"] text')) {
      expect(Number(t.getAttribute('font-size')) * pxPerCm(near)).toBeGreaterThanOrEqual(
        LABEL_MIN_PX,
      );
    }

    rerender(
      <HarborMap
        meta={meta}
        scene={scene}
        viewport={far}
        layers={{ ...DEFAULT_LAYERS, labels: false }}
      />,
    );
    expect(screen.queryByTestId('structure-label')).toBeNull();
  });

  it('hides free text decorations that would be smaller than LABEL_MIN_PX', () => {
    const deco: Decoration = {
      key: 't',
      kind: 'text',
      label: { it: 'Distributore' },
      position: { x: 1000, y: 1000 },
      size_cm: 200,
    };
    const scene = buildScene([{ kind: 'decoration', el: deco }]);
    const far = fitToView({ minX: 0, minY: 0, maxX: 60_000, maxY: 40_000 }, 800, 600);
    const { rerender } = renderWithProviders(
      <HarborMap meta={meta} scene={scene} viewport={far} />,
    );
    expect(screen.queryByText('Distributore')).toBeNull();
    const near = fitToView({ minX: 0, minY: 0, maxX: 4000, maxY: 3000 }, 800, 600);
    rerender(<HarborMap meta={meta} scene={scene} viewport={near} />);
    expect(screen.getByText('Distributore')).toBeInTheDocument();
  });
});

describe('structureLabelPlacement', () => {
  it('puts the name halfway along a pier or quay and at the centre of a closed outline', () => {
    const down = structureLabelPlacement({ kind: 'pier', code: 'A' }, [
      { x: 0, y: 0 },
      { x: 0, y: 1000 },
    ]);
    expect(down).toEqual({ x: 0, y: 500 });
    const left = structureLabelPlacement({ kind: 'quay', code: 'Q' }, [
      { x: 1000, y: 0 },
      { x: 0, y: 0 },
    ]);
    expect(left).toEqual({ x: 500, y: 0 });
    const closed = structureLabelPlacement({ kind: 'breakwater', code: 'D', closed: true }, [
      { x: 0, y: 0 },
      { x: 200, y: 0 },
      { x: 200, y: 100 },
      { x: 0, y: 100 },
    ]);
    expect(closed).toEqual({ x: 100, y: 50 });
  });
});
