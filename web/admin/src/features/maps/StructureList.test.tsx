import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { Structure } from '@porto/shared/api';
import { buildScene, type SceneEntry } from '@porto/shared/map';
import { renderWithProviders } from '@porto/shared/testing';

import { groupBerthsByStructure, mapAspectClass } from './lib';
import { StructureList } from './StructureList';
import { berth } from './testFixtures';

const structure = (
  id: string,
  kind: Structure['kind'],
  code: string,
  name?: string,
): SceneEntry => ({
  kind: 'structure',
  el: {
    id,
    kind,
    code,
    name,
    points: [
      { x: 0, y: 0 },
      { x: 0, y: 1000 },
    ],
    width_cm: 300,
  },
});

function scene() {
  return buildScene([
    structure('quay', 'quay', 'BANCHINA', 'Banchina di riva'),
    structure('pier-b', 'pier', 'B'),
    structure('pier-a', 'pier', 'A'),
    structure('pier-t', 'pier', 'T', 'Pontile dinamico'),
    { kind: 'berth', el: berth('a1', { code: 'A-1', structure_id: 'pier-a' }) },
    { kind: 'berth', el: berth('b1', { code: 'B-1', structure_id: 'pier-b' }) },
    {
      kind: 'berth',
      el: berth('boa1', {
        code: 'BOA-1',
        structure_id: undefined,
        mooring_type: 'buoy',
        swing_radius_cm: 900,
      }),
    },
    { kind: 'berth', el: berth('x1', { code: 'X-1', structure_id: undefined }) },
  ]);
}

describe('groupBerthsByStructure', () => {
  it('orders structures with berths, then buoys and loose berths, then empty structures', () => {
    const groups = groupBerthsByStructure(scene());
    expect(groups.map((g) => [g.kind, g.code, g.berths.length])).toEqual([
      ['pier', 'A', 1],
      ['pier', 'B', 1],
      ['buoys', '', 1],
      ['loose', '', 1],
      ['quay', 'BANCHINA', 0],
      ['pier', 'T', 0],
    ]);
  });
});

describe('StructureList', () => {
  it('shows structure names and kinds instead of raw codes, muting the empty ones', () => {
    renderWithProviders(<StructureList items={scene()} />);
    expect(screen.getByRole('button', { name: /Pontile A/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Campo boe/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Posti senza struttura/ })).toBeInTheDocument();
    const quay = screen.getByRole('button', { name: /Banchina di riva/ });
    expect(quay).toHaveTextContent('Banchina BANCHINA');
    expect(quay).toHaveTextContent('Posti: 0');
    expect(quay).toHaveClass('text-muted-foreground');
    expect(screen.getByRole('button', { name: /Pontile dinamico/ })).toHaveTextContent('Pontile T');
    expect(screen.queryByText(/Struttura/)).toBeNull();
    expect(screen.queryByText(/Non agganciati/)).toBeNull();
  });
});

describe('mapAspectClass', () => {
  it('picks the aspect class nearest to the map proportions', () => {
    expect(mapAspectClass(60_000, 40_000)).toBe('aspect-3/2');
    expect(mapAspectClass(10_000, 10_000)).toBe('aspect-square');
    expect(mapAspectClass(160_000, 90_000)).toBe('aspect-video');
    expect(mapAspectClass(10_000, 50_000)).toBe('aspect-1/2');
    expect(mapAspectClass(0, 0)).toBe('aspect-4/3');
  });
});
