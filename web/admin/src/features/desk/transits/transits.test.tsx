import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { problem } from '@porto/shared/testing';

import { MARINA_ID, renderApp } from '../../../test/renderApp';
import { pickOption } from '../../pricing/test/dom';
import { transitTabFilters, transitBody, stepReady, type TransitDraft } from '../lib/transit';
import {
  API,
  B2,
  CUSTOMER_ID,
  TRANSIT_ID,
  baseRoutes,
  meDesk,
  solution,
  transit,
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

describe('transit wizard steps', () => {
  const d: TransitDraft = {
    vesselId: 'v',
    newVessel: null,
    customerId: null,
    arrival: '2026-07-10',
    departure: '2026-07-12',
    solution: null,
    target: { resource_type: 'berth', resource_id: 'b' },
    expectedArrivalTime: '15:30',
    notes: ' ',
    confirm: null,
    fitOverrideReason: null,
  };

  it('gates each step and builds the manual-driver body', () => {
    expect(stepReady('vessel', { ...d, vesselId: null })).toBe(false);
    expect(stepReady('stay', { ...d, departure: '2026-07-10' })).toBe(false);
    expect(stepReady('confirm', { ...d, confirm: { reference: '', payAtDeparture: true } })).toBe(
      false,
    );
    expect(
      transitBody({
        ...d,
        customerId: 'c',
        confirm: { reference: ' POS 123 ', payAtDeparture: false },
        fitOverrideReason: 'ok',
      }),
    ).toEqual({
      arrival: '2026-07-10',
      departure: '2026-07-12',
      channel: 'desk',
      vessel_id: 'v',
      customer_id: 'c',
      expected_arrival_time: '15:30',
      placements: [
        {
          resource_type: 'berth',
          resource_id: 'b',
          first_night: '2026-07-10',
          last_night: '2026-07-11',
        },
      ],
      confirm: { pay_at_departure: false, reference: 'POS 123' },
      fit_override: { reason: 'ok' },
    });
    expect(
      transitBody({ ...d, solution: solution('k', 'A02', B2) })!.placements![0]!.resource_id,
    ).toBe(B2);
  });

  it('maps the list tabs to filters', () => {
    expect(transitTabFilters('today', '2026-07-10')).toEqual({
      arrival_from: '2026-07-10',
      arrival_to: '2026-07-10',
    });
    expect(transitTabFilters('upcoming', '2026-07-10')).toEqual({ arrival_from: '2026-07-11' });
    expect(transitTabFilters('inPort', '2026-07-10')).toEqual({ status: 'arrived' });
  });
});

describe('transit pages', () => {
  it('lists today arrivals and switches tab', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/transits`, [
      ...baseRoutes(),
      {
        path: `${API}/transits`,
        body: {
          items: [
            {
              id: TRANSIT_ID,
              code: 'TRN-2026-0001',
              kind: 'transit',
              status: 'held',
              vessel: transit().vessel,
              starts_on: '2026-07-10',
              ends_on: '2026-07-13',
              total_cents: 12000,
              hold_lost: false,
              resources: [{ resource_type: 'berth', resource_id: 'b1', code: 'A01' }],
              created_at: '2026-07-10T08:00:00Z',
              version: 1,
            },
          ],
          next_cursor: null,
        },
      },
    ]);
    expect(await screen.findByRole('link', { name: 'TRN-2026-0001' })).toBeInTheDocument();
    expect(screen.getByText('In opzione')).toBeInTheDocument();
    expect(
      calls.find((c) => c.url.pathname.endsWith('/transits'))!.url.searchParams.get('arrival_to'),
    ).toBe('2026-07-10');
    await userEvent.click(screen.getByRole('radio', { name: 'In porto' }));
    await waitFor(() =>
      expect(calls.some((c) => c.url.searchParams.get('status') === 'arrived')).toBe(true),
    );
  });

  it('confirms a held transit with the manual driver after linking the customer', async () => {
    let linked = false;
    const { calls } = renderApp(`/m/${MARINA_ID}/transits/${TRANSIT_ID}`, [
      ...baseRoutes(),
      {
        path: (u) => u.pathname === `${API}/contracts/${TRANSIT_ID}`,
        body: transit(),
      },
      {
        method: 'PATCH',
        path: (u) => {
          if (u.pathname !== `${API}/transits/${TRANSIT_ID}`) return false;
          linked = true;
          return true;
        },
        body: transit({ customer: { id: CUSTOMER_ID, display_name: 'Mario Rossi' } }),
      },
      {
        method: 'POST',
        path: `${API}/transits/${TRANSIT_ID}/confirm`,
        body: transit({ status: 'confirmed' }),
      },
    ]);
    expect(await screen.findByRole('heading', { name: 'TRN-2026-0001' })).toBeInTheDocument();
    expect(screen.getByTestId('desk-transit-hold')).toHaveTextContent('In opzione fino al');
    expect(screen.queryByRole('button', { name: 'Non arrivato' })).toBeNull();
    await userEvent.click(screen.getByRole('button', { name: 'Conferma' }));
    const dialog = await screen.findByTestId('desk-confirm-dialog');
    expect(within(dialog).getByText('Serve un cliente per confermare.')).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: 'Conferma il transito' })).toBeDisabled();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Annulla' }));
    await userEvent.click(screen.getByRole('button', { name: 'Collega il cliente' }));
    const link = await screen.findByRole('dialog');
    await pickOption('Cliente', 'Mario Rossi', link);
    await userEvent.click(within(link).getByRole('button', { name: 'Salva' }));
    await waitFor(() => expect(linked).toBe(true));
    expect(calls.find((c) => c.method === 'PATCH')!.body).toEqual({ customer_id: CUSTOMER_ID });
  });

  it('sends the manual confirmation with reference and pay-at-departure', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/transits/${TRANSIT_ID}`, [
      ...baseRoutes(),
      {
        path: `${API}/contracts/${TRANSIT_ID}`,
        body: transit({ customer: { id: CUSTOMER_ID, display_name: 'Mario Rossi' } }),
      },
      {
        method: 'POST',
        path: `${API}/transits/${TRANSIT_ID}/confirm`,
        body: transit({ status: 'confirmed' }),
      },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Conferma' }));
    const dialog = await screen.findByTestId('desk-confirm-dialog');
    await userEvent.type(within(dialog).getByLabelText('Riferimento del pagamento'), 'POS 42');
    await userEvent.click(within(dialog).getByLabelText('Paga alla partenza'));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Conferma il transito' }));
    await waitFor(() => expect(calls.some((c) => c.url.pathname.endsWith('/confirm'))).toBe(true));
    const call = calls.find((c) => c.url.pathname.endsWith('/confirm'))!;
    expect(call.headers.get('If-Match')).toBe('"4"');
    expect(call.body).toEqual({ pay_at_departure: true, reference: 'POS 42' });
  });

  it('shows a stay-change conflict with its alternatives and retries on the chosen one', async () => {
    let n = 0;
    const { calls } = renderApp(`/m/${MARINA_ID}/transits/${TRANSIT_ID}`, [
      ...baseRoutes(meDesk()),
      { path: `${API}/contracts/${TRANSIT_ID}`, body: transit({ status: 'confirmed' }) },
      {
        method: 'POST',
        path: (u) => u.pathname.endsWith('/stay') && n++ === 0,
        status: 409,
        body: problem('resource_taken', 409, {
          details: {
            resource_type: 'berth',
            resource_id: 'b1',
            from: '2026-07-13',
            to: '2026-07-13',
            allocation_ids: ['z'],
            alternatives: [solution('alt', 'A02', B2)],
          },
        }),
      },
      {
        method: 'POST',
        path: (u) => u.pathname.endsWith('/stay'),
        body: {
          contract: transit(),
          price_effect: { old_total_cents: 1, new_total_cents: 2, delta_cents: 1 },
        },
      },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Cambia soggiorno' }));
    const dialog = await screen.findByTestId('desk-stay-dialog');
    // The planner and the day view change dates by drag; here the departure is kept and a save forces the call.
    await userEvent.click(within(dialog).getByRole('button', { name: 'Salva' }));
    expect(
      await within(dialog).findByText(
        'Il posto è già occupato in alcune notti del periodo richiesto.',
      ),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByText('Notti in conflitto: dal 13/07/2026 al 13/07/2026'),
    ).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: /A02/ }));
    await waitFor(() =>
      expect(calls.filter((c) => c.url.pathname.endsWith('/stay'))).toHaveLength(2),
    );
    expect(calls.filter((c) => c.url.pathname.endsWith('/stay'))[1]!.body).toEqual({
      extension_target: { resource_type: 'berth', resource_id: B2 },
    });
  });
});
