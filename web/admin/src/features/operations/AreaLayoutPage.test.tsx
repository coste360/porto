import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it } from 'vitest';

import type { AreaPlacementData, DynamicArea } from '@porto/shared/api';
import { problem, type MockRoute } from '@porto/shared/testing';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../test/renderApp';
import { makeMapDocument } from '../maps/testFixtures';
import { ALL_OPS, DATE, base, makeDayView, summary } from './testFixtures';

const area: DynamicArea = {
  id: 'area-1',
  kind: 'linear',
  code: 'L1',
  structure_id: 'pier-a',
  side: 'left',
  from_cm: 0,
  to_cm: 5000,
  mooring_mode: 'stern_to',
  cell_cm: 100,
  margin_cm: 100,
  blocked_cells: [],
  exits: [],
  grid_rotation_dd: 0,
  active: true,
  group_ids: [],
};
const doc = makeMapDocument({ dynamic_areas: [area] });

const placement = (id: string, name: string, x: number, version = 1): AreaPlacementData => ({
  area_id: 'area-1',
  allocation_id: id,
  allocation_version: version,
  x_cm: x,
  cells: { first: x / 100, last: x / 100 + 4 },
  status: 'transit',
  kind: 'transit',
  vessel: { id: `v-${id}`, name, loa_cm: 1000, beam_cm: 350, hull_type: 'monohull_sail' },
  anomalies: [],
});

const day = makeDayView({
  area_placements: [placement('al-a', 'Alfa', 0), placement('al-b', 'Beta', 500)],
});

function routes(extra: MockRoute[] = []): MockRoute[] {
  return [
    meRoute(makeMe({}, ALL_OPS)),
    { path: `${base}/maps`, body: { items: [summary(doc)] } },
    { path: `${base}/maps/map-1`, body: doc },
    { path: `${base}/maps/map-1/day`, body: day },
    ...extra,
  ];
}

const path = `/m/${MARINA_ID}/ops/area/area-1?date=${DATE}`;

afterEach(() => window.sessionStorage.clear());

describe('AreaLayoutPage', () => {
  it('lists the vessels of the area; taking one out makes it "to place" and blocks the save', async () => {
    renderApp(path, routes());
    expect(await screen.findByRole('heading', { name: 'Disposizione di L1' })).toBeInTheDocument();
    expect(await screen.findByText('Alfa')).toBeInTheDocument();
    expect(screen.getByTestId('ops-unplaced-count')).toHaveTextContent('0');
    expect(screen.getByTestId('ops-layout-save')).toBeDisabled();
    await userEvent.click(screen.getByTestId('ops-layout-remove-al-a'));
    expect(screen.getByTestId('ops-unplaced-count')).toHaveTextContent('1');
    expect(screen.getByTestId('ops-to-place-al-a')).toBeInTheDocument();
    expect(screen.getByTestId('ops-layout-save')).toBeDisabled();
    // The draft survives a reload of the page (sessionStorage, per user, area and night).
    expect(window.sessionStorage.getItem(`porto:ops:layout:u1:area-1:${DATE}`)).toContain('remove');
    await userEvent.click(screen.getByTestId('ops-layout-restore'));
    expect(screen.getByTestId('ops-unplaced-count')).toHaveTextContent('0');
  });

  it('applies a compaction proposal and saves every vessel of the draft with the base versions', async () => {
    const { calls } = renderApp(
      path,
      routes([
        {
          method: 'POST',
          path: `${base}/dynamic-areas/area-1/compact`,
          body: { placements: [{ allocation_id: 'al-b', x_cm: 450 }], freed_cm: 50, base: [] },
        },
        {
          method: 'POST',
          path: `${base}/dynamic-areas/area-1/layout`,
          body: { allocations: [], undo_id: 'u-1', undo_until: '2026-11-08T08:00:00Z' },
        },
      ]),
    );
    await userEvent.click(await screen.findByTestId('ops-layout-compact'));
    await waitFor(() => expect(screen.getByTestId('ops-layout-save')).toBeEnabled());
    await userEvent.click(screen.getByTestId('ops-layout-save'));
    await waitFor(() => expect(calls.some((c) => c.url.pathname.endsWith('/layout'))).toBe(true));
    const saveCall = calls.find((c) => c.url.pathname.endsWith('/layout'))!;
    expect(saveCall.body).toEqual({
      date: DATE,
      base: [
        { allocation_id: 'al-a', version: 1 },
        { allocation_id: 'al-b', version: 1 },
      ],
      placements: [
        { allocation_id: 'al-a', x_cm: 0 },
        { allocation_id: 'al-b', x_cm: 450 },
      ],
    });
    expect(saveCall.headers.get('Idempotency-Key')).toBeTruthy();
  });

  it('a stale save reloads the base, re-applies the draft and names the changed vessels', async () => {
    const { calls } = renderApp(
      path,
      routes([
        {
          method: 'POST',
          path: `${base}/dynamic-areas/area-1/compact`,
          body: { placements: [{ allocation_id: 'al-b', x_cm: 450 }], freed_cm: 50, base: [] },
        },
        {
          method: 'POST',
          path: `${base}/dynamic-areas/area-1/layout`,
          status: 412,
          body: problem('version_conflict', 412),
        },
      ]),
    );
    await userEvent.click(await screen.findByTestId('ops-layout-compact'));
    await waitFor(() => expect(screen.getByTestId('ops-layout-save')).toBeEnabled());
    await userEvent.click(screen.getByTestId('ops-layout-save'));
    await waitFor(() =>
      expect(calls.filter((c) => c.url.pathname.endsWith('/day')).length).toBeGreaterThan(1),
    );
    // Same base on the server in this test: nothing changed, the draft is kept.
    expect(screen.getByTestId('ops-layout-save')).toBeEnabled();
    expect(within(screen.getByTestId('ops-layout-dirty')).getByText(/1/)).toBeInTheDocument();
  });
});
