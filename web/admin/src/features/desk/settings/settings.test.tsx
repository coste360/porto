import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { MARINA_ID, renderApp } from '../../../test/renderApp';
import { pickOption } from '../../pricing/test/dom';
import { API, baseRoutes } from '../test/fixtures';

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

const settings = {
  safety_clearance_cm: 30,
  waste_threshold_bp: 6000,
  draft_hold_days: 7,
  desk_hold_hours: 24,
  quote_validity_days: 7,
  max_segments: 1,
  absent_days_threshold: 7,
  arrival_grace_minutes: 120,
  default_arrival_time: '14:00',
  default_departure_time: '11:00',
  reprice_on_move: false,
  release_policy: 'with_consent',
  version: 3,
};

describe('occupancy settings', () => {
  it('saves the rules with If-Match and refuses out-of-range values', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/occupancy-settings`, [
      ...baseRoutes(),
      { path: `${API}/occupancy-settings`, body: settings },
      { method: 'PUT', path: `${API}/occupancy-settings`, body: { ...settings, version: 4 } },
      { path: `${API}/closures`, body: { items: [] } },
    ]);
    const form = await screen.findByTestId('desk-occupancy-settings');
    const hours = within(form).getByLabelText('Ore di opzione di un transito al banco');
    await userEvent.clear(hours);
    await userEvent.type(hours, '500');
    await userEvent.click(within(form).getByRole('button', { name: 'Salva' }));
    expect(
      await within(form).findByText("Valore fuori dall'intervallo ammesso."),
    ).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'PUT')).toBe(false);
    await userEvent.clear(hours);
    await userEvent.type(hours, '48');
    await userEvent.click(within(form).getByRole('switch'));
    await userEvent.click(within(form).getByRole('button', { name: 'Salva' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const put = calls.find((c) => c.method === 'PUT')!;
    expect(put.headers.get('If-Match')).toBe('"3"');
    const { version: _v, ...rest } = settings;
    expect(put.body).toEqual({ ...rest, desk_hold_hours: 48, reprice_on_move: true });
  });

  it('adds a marina-wide closure with a reason', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/occupancy-settings`, [
      ...baseRoutes(),
      { path: `${API}/occupancy-settings`, body: settings },
      {
        path: `${API}/closures`,
        body: {
          items: [
            {
              id: 'c1',
              scope_type: 'marina',
              from_date: '2026-07-18',
              to_date: '2026-07-19',
              reason: 'Regata',
              version: 1,
            },
          ],
        },
      },
      { method: 'POST', path: `${API}/closures`, status: 201, body: { id: 'c2' } },
      { method: 'DELETE', path: `${API}/closures/c1`, status: 204 },
    ]);
    expect(await screen.findByText('Regata')).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText('Motivo'), 'Lavori in banchina');
    await userEvent.click(screen.getByRole('button', { name: 'Aggiungi la chiusura' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    expect(calls.find((c) => c.method === 'POST')!.body).toEqual({
      scope_type: 'marina',
      from_date: '2026-07-10',
      to_date: '2026-07-10',
      reason: 'Lavori in banchina',
    });
    await userEvent.click(screen.getByRole('button', { name: 'Elimina la chiusura' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE')).toBe(true));
    expect(calls.find((c) => c.method === 'DELETE')!.headers.get('If-Match')).toBe('"1"');
  });
});

describe('inquiries', () => {
  it('lists inquiries and edits the outcome inline', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/inquiries`, [
      ...baseRoutes(),
      {
        path: `${API}/inquiries`,
        body: {
          items: [
            {
              id: 'i1',
              channel: 'desk',
              source: 'phone',
              purpose: 'transit',
              loa_cm: 1500,
              beam_cm: 450,
              arrival: '2026-08-01',
              departure: '2026-08-04',
              outcome: 'open',
              created_at: '2026-07-09T10:00:00Z',
              version: 2,
            },
          ],
          next_cursor: null,
        },
      },
      { method: 'PATCH', path: `${API}/inquiries/i1`, body: {} },
    ]);
    expect(await screen.findByText('15,00 m × 4,50 m')).toBeInTheDocument();
    await pickOption('Esito', 'Rifiutata: nessun posto', screen.getByRole('table'));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    const patch = calls.find((c) => c.method === 'PATCH')!;
    expect(patch.headers.get('If-Match')).toBe('"2"');
    expect(patch.body).toEqual({ outcome: 'declined_no_space' });
  });
});
