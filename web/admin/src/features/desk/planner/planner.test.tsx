import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { MARINA_ID, renderApp } from '../../../test/renderApp';
import { COL_PX, HEADER_PX, LABEL_PX, ROW_PX } from '../lib/planner';
import {
  ALLOC_ID,
  API,
  B1,
  B2,
  MAP_ID,
  TRANSIT_ID,
  baseRoutes,
  transit,
  vesselBrief,
} from '../test/fixtures';

vi.mock('../../operations/public', () => ({
  opsMapSearch: () => ({}),
  PlacementPicker: () => null,
}));
vi.mock('../../operations/routes', () => ({ operationsRoutes: () => [] }));
vi.mock('../../maps/routes', () => ({ mapRoutes: () => [] }));

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-07-10T08:00:00Z'));
});
afterEach(() => vi.useRealTimers());

const occupancy = {
  from: '2026-07-10',
  to: '2026-07-23',
  allocations: [
    {
      id: ALLOC_ID,
      resource_type: 'berth',
      resource_id: B1,
      first_night: '2026-07-10',
      last_night: '2026-07-12',
      kind: 'transit',
      status: 'active',
      version: 2,
      released: [],
      vessel: vesselBrief,
      contract: { id: TRANSIT_ID, kind: 'transit', status: 'confirmed', code: 'TRN-2026-0001' },
    },
  ],
};

const routes = () => [
  ...baseRoutes(),
  { path: `${API}/maps/${MAP_ID}/occupancy`, body: occupancy },
  { path: `${API}/contracts/${TRANSIT_ID}`, body: transit({ status: 'confirmed', version: 7 }) },
  {
    method: 'POST',
    path: `${API}/transits/${TRANSIT_ID}/stay`,
    body: {
      contract: transit(),
      price_effect: { old_total_cents: 1, new_total_cents: 2, delta_cents: 1 },
    },
  },
  {
    method: 'POST',
    path: `${API}/occupancies/move`,
    body: { allocations: [], undo_id: 'u', undo_until: '2026-07-11T08:00:00Z' },
  },
];

describe('planner', () => {
  it('draws rows and bars on night columns and asks the occupancy of the visible range', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/planner`, routes());
    const grid = await screen.findByTestId('desk-planner');
    expect(within(grid).getByTestId('desk-planner-row-A01')).toBeInTheDocument();
    expect(within(grid).getByTestId('desk-planner-row-L1')).toBeInTheDocument();
    const bar = within(grid).getByRole('button', {
      name: /TRN-2026-0001, Libeccio, dal 10\/07\/2026 al 12\/07\/2026/,
    });
    const rect = bar.querySelector('rect')!;
    expect(rect.getAttribute('x')).toBe(String(LABEL_PX + 1));
    expect(rect.getAttribute('width')).toBe(String(3 * COL_PX - 2));
    expect(rect.getAttribute('class')).toContain('fill-harbor-transit');
    const q = calls.find((c) => c.url.pathname.endsWith('/occupancy'))!.url.searchParams;
    expect([q.get('from'), q.get('to')]).toEqual(['2026-07-10', '2026-07-23']);
  });

  it('extends a transit by dragging the right edge of its bar (stay change with the contract version)', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/planner`, routes());
    const grid = await screen.findByTestId('desk-planner');
    const bar = within(grid).getByTestId('desk-planner-bar');
    const right = LABEL_PX + 3 * COL_PX - 3;
    fireEvent.pointerDown(bar, { clientX: right, clientY: HEADER_PX + 10 });
    fireEvent.pointerMove(grid, { clientX: right + 2 * COL_PX, clientY: HEADER_PX + 10 });
    fireEvent.pointerUp(grid, { clientX: right + 2 * COL_PX, clientY: HEADER_PX + 10 });
    const dialog = await screen.findByTestId('desk-planner-change');
    expect(within(dialog).getByText(/dal 10\/07\/2026 al 15\/07\/2026/)).toBeInTheDocument();
    const confirm = within(dialog).getByRole('button', { name: 'Conferma' });
    await waitFor(() => expect(confirm).toBeEnabled());
    await userEvent.click(confirm);
    await waitFor(() => expect(calls.some((c) => c.url.pathname.endsWith('/stay'))).toBe(true));
    const stay = calls.find((c) => c.url.pathname.endsWith('/stay'))!;
    expect(stay.headers.get('If-Match')).toBe('"7"');
    expect(stay.body).toEqual({ departure: '2026-07-15' });
  });

  it('moves a vessel to another berth by dragging the bar down one row', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/planner`, routes());
    const grid = await screen.findByTestId('desk-planner');
    const bar = within(grid).getByTestId('desk-planner-bar');
    fireEvent.pointerDown(bar, { clientX: LABEL_PX + 10, clientY: HEADER_PX + 10 });
    fireEvent.pointerMove(grid, { clientX: LABEL_PX + 10, clientY: HEADER_PX + 10 + ROW_PX });
    fireEvent.pointerUp(grid, { clientX: LABEL_PX + 10, clientY: HEADER_PX + 10 + ROW_PX });
    const dialog = await screen.findByTestId('desk-planner-change');
    expect(within(dialog).getByText(/Libeccio va su A02/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Sposta' }));
    await waitFor(() =>
      expect(calls.some((c) => c.url.pathname.endsWith('/occupancies/move'))).toBe(true),
    );
    expect(calls.find((c) => c.url.pathname.endsWith('/occupancies/move'))!.body).toEqual({
      allocation_id: ALLOC_ID,
      version: 2,
      target: { resource_type: 'berth', resource_id: B2 },
      from_night: '2026-07-10',
    });
  });

  it('extends from the keyboard with Alt+→ and opens a new transit on free space', async () => {
    renderApp(`/m/${MARINA_ID}/planner`, routes());
    const grid = await screen.findByTestId('desk-planner');
    const bar = within(grid).getByTestId('desk-planner-bar');
    bar.focus();
    fireEvent.keyDown(bar, { key: 'ArrowRight', altKey: true });
    const dialog = await screen.findByTestId('desk-planner-change');
    expect(within(dialog).getByText(/al 14\/07\/2026/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Annulla' }));
    const row = within(grid).getByTestId('desk-planner-row-A02').querySelector('rect')!;
    fireEvent.pointerDown(row, {
      clientX: LABEL_PX + 2 * COL_PX + 5,
      clientY: HEADER_PX + ROW_PX + 5,
    });
    fireEvent.pointerMove(grid, {
      clientX: LABEL_PX + 4 * COL_PX + 5,
      clientY: HEADER_PX + ROW_PX + 5,
    });
    fireEvent.pointerUp(grid, {
      clientX: LABEL_PX + 4 * COL_PX + 5,
      clientY: HEADER_PX + ROW_PX + 5,
    });
    const wizard = await screen.findByTestId('desk-transit-dialog');
    expect(within(wizard).getByText('Passo 1 di 3 · Imbarcazione e cliente')).toBeInTheDocument();
  });
});
