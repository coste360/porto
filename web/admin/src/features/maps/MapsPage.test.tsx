import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import type { MapSummary } from '@porto/shared/api';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../test/renderApp';
import { moveId, parseMapExport } from './lib';

const base = `/api/admin/v1/marinas/${MARINA_ID}/maps`;

const summary = (id: string, over: Partial<MapSummary> = {}): MapSummary => ({
  id,
  kind: 'water',
  name: { it: `Mappa ${id}` },
  icon: '',
  width_cm: 60_000,
  height_cm: 40_000,
  sort_order: 0,
  version: 4,
  counts: {
    structures: 6,
    berths: 400,
    dynamic_areas: 0,
    land_resources: 0,
    pedestals: 0,
    decorations: 0,
  },
  georeferenced: false,
  has_background: false,
  updated_at: '2026-11-06T10:00:00Z',
  ...over,
});

const owner = makeMe({}, ['marina.read', 'map.view', 'harbor.configure']);
const viewer = makeMe({}, ['marina.read', 'map.view']);

describe('MapsPage', () => {
  it('lists the maps with their counts', async () => {
    renderApp(`/m/${MARINA_ID}/maps`, [
      meRoute(owner),
      { path: base, body: { items: [summary('m2', { sort_order: 1 }), summary('m1')] } },
    ]);
    const list = await screen.findByRole('list', { name: 'Mappe' });
    const items = within(list).getAllByRole('listitem');
    expect(items[0]).toHaveTextContent('Mappa m1');
    expect(items[0]).toHaveTextContent('Posti: 400');
    expect(screen.getByRole('button', { name: /Nuova mappa/ })).toBeInTheDocument();
  });

  it('hides the write actions without harbor.configure', async () => {
    renderApp(`/m/${MARINA_ID}/maps`, [
      meRoute(viewer),
      { path: base, body: { items: [summary('m1')] } },
    ]);
    expect(await screen.findByText('Mappa m1')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Nuova mappa/ })).not.toBeInTheDocument();
  });

  it('creates a map in integer cm with an Idempotency-Key', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/maps`, [
      meRoute(owner),
      { path: base, body: { items: [] } },
      { method: 'POST', path: base, status: 201, body: { id: 'new-map' } },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: /Nuova mappa/ }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Nome (italiano)'), 'Porto');
    const width = within(dialog).getByLabelText('Larghezza');
    await userEvent.clear(width);
    await userEvent.type(width, '800');
    const height = within(dialog).getByLabelText('Altezza');
    await userEvent.clear(height);
    await userEvent.type(height, '500,5');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Crea' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    const post = calls.find((c) => c.method === 'POST');
    expect(post?.body).toEqual({
      kind: 'water',
      name: { it: 'Porto' },
      width_cm: 80_000,
      height_cm: 50_050,
      grid_cm: 50,
    });
    expect(post?.headers.get('Idempotency-Key')).toBeTruthy();
  });

  it('reorders from the card menu', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/maps`, [
      meRoute(owner),
      { path: base, body: { items: [summary('m1'), summary('m2', { sort_order: 1 })] } },
      { method: 'POST', path: `${base}/reorder`, status: 204 },
    ]);
    await screen.findByText('Mappa m1');
    await userEvent.click(screen.getAllByRole('button', { name: 'Azioni' })[0]!);
    await userEvent.click(await screen.findByRole('menuitem', { name: /Sposta dopo/ }));
    await waitFor(() =>
      expect(calls.find((c) => c.url.pathname.endsWith('/reorder'))?.body).toEqual({
        map_ids: ['m2', 'm1'],
      }),
    );
  });

  it('deletes with If-Match', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/maps`, [
      meRoute(owner),
      { path: base, body: { items: [summary('m1')] } },
      { method: 'DELETE', path: `${base}/m1`, status: 204 },
    ]);
    await screen.findByText('Mappa m1');
    await userEvent.click(screen.getByRole('button', { name: 'Azioni' }));
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Elimina' }));
    const alert = await screen.findByRole('alertdialog');
    await userEvent.click(within(alert).getByRole('button', { name: 'Elimina' }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === 'DELETE')?.headers.get('If-Match')).toBe('"4"'),
    );
  });
});

describe('maps lib', () => {
  it('validates exported files and moves ids', () => {
    expect(parseMapExport('{"format":"porto-map/1","map":{},"groups":[]}')).not.toBeNull();
    expect(parseMapExport('{"format":"other"}')).toBeNull();
    expect(parseMapExport('nope')).toBeNull();
    expect(moveId(['a', 'b', 'c'], 'c', 0)).toEqual(['c', 'a', 'b']);
  });
});
