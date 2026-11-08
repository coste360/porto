import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { UtilityMeter } from '@porto/shared/api';

import { MARINA_ID, makeMe, renderApp } from '../../test/renderApp';
import { API, B1, baseRoutes } from '../desk/test/fixtures';
import { expectedRange } from '../phone/readings/range';

const meter: UtilityMeter = {
  id: '0190a000-0000-7000-8000-00000000c001',
  code: 'A01-E',
  kind: 'power',
  unit: 'kwh',
  resource_type: 'berth',
  resource_id: B1,
  multiplier_milli: 1000,
  active: true,
  version: 1,
  last_reading: {
    id: 'r1',
    read_at: '2026-11-03T09:00:00Z',
    read_on: '2026-11-03',
    value_milli: 1_000_000,
  },
  created_at: '2026-11-01T10:00:00Z',
  updated_at: '2026-11-01T10:00:00Z',
};

describe('expectedRange', () => {
  it('allows up to the daily threshold per day since the previous reading', () => {
    expect(expectedRange({ ...meter, daily_threshold_milli: 10_000 }, '2026-11-08')).toEqual({
      min: 1_000_000,
      max: 1_050_000,
    });
    expect(expectedRange(meter, '2026-11-03')).toEqual({ min: 1_000_000, max: 1_030_000 });
    expect(expectedRange({ ...meter, kind: 'hours' }, '2026-11-08')).toEqual({
      min: 1_000_000,
      max: null,
    });
    expect(expectedRange({ ...meter, last_reading: undefined }, '2026-11-08')).toBeNull();
  });
});

describe('utilities pages', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-08T10:00:00Z'));
  });
  afterEach(() => vi.useRealTimers());

  it('shows the previous value, the expected range and warns outside it in the dock Letture tab', async () => {
    renderApp(`/m/${MARINA_ID}/dock?tab=readings`, [
      ...baseRoutes(makeMe({}, ['marina.read', 'presence.write', 'map.view'])),
      { path: `${API}/utility-meters`, body: { items: [meter], next_cursor: null } },
    ]);
    const tab = await screen.findByTestId('readings-tab');
    await userEvent.type(within(tab).getByLabelText(/Cerca contatore/), 'a01');
    await userEvent.click(await screen.findByTestId('dock-meter'));
    expect(screen.getByTestId('previous-reading')).toHaveTextContent('1.000 kWh del 03/11/2026');
    expect(screen.getByTestId('expected-range')).toHaveTextContent('Atteso tra 1.000 e 1.150 kWh');
    const input = screen.getByLabelText('Indice (kWh)');
    await userEvent.type(input, '1200,5');
    expect(screen.getByTestId('out-of-range')).toBeInTheDocument();
    await userEvent.clear(input);
    await userEvent.type(input, '1012,25');
    expect(screen.queryByTestId('out-of-range')).toBeNull();
    expect(screen.getByRole('button', { name: 'Salva lettura' })).toBeEnabled();
  });

  it('confirms an anomalous reading with If-Match', async () => {
    const anomalous = {
      id: 'r2',
      meter_id: meter.id,
      read_at: '2026-11-07T09:00:00Z',
      read_on: '2026-11-07',
      value_milli: 900_000,
      kind: 'regular',
      source: 'staff',
      status: 'anomaly',
      anomaly_codes: ['negative'],
      version: 2,
      created_at: '2026-11-07T09:00:00Z',
    };
    const { calls } = renderApp(`/m/${MARINA_ID}/utilities/readings`, [
      ...baseRoutes(makeMe({}, ['marina.read', 'marina.configure', 'charges.manage'])),
      { path: `${API}/utility-meters`, body: { items: [meter], next_cursor: null } },
      { path: `${API}/meter-readings`, body: { items: [anomalous], next_cursor: null } },
      {
        method: 'POST',
        path: `${API}/meter-readings/r2/confirm`,
        body: { ...anomalous, status: 'confirmed', version: 3 },
      },
    ]);
    const row = await screen.findByTestId('reading-row');
    expect(row).toHaveTextContent('Anomala');
    expect(row).toHaveTextContent('Indice inferiore');
    await userEvent.click(within(row).getByRole('button', { name: 'Conferma' }));
    await waitFor(() => expect(calls.some((c) => c.url.pathname.endsWith('/confirm'))).toBe(true));
    expect(calls.find((c) => c.url.pathname.endsWith('/confirm'))!.headers.get('If-Match')).toBe(
      '"2"',
    );
  });

  it('runs the utility charges and shows the summary', async () => {
    renderApp(`/m/${MARINA_ID}/utilities/consumptions`, [
      ...baseRoutes(makeMe({}, ['marina.read', 'charges.manage'])),
      { path: `${API}/utility-consumptions`, body: { items: [], next_cursor: null } },
      {
        method: 'POST',
        path: `${API}/utility-charges/run`,
        body: {
          charges: [
            { charge_id: 'c1', billing_id: 'b1', contract_id: 'k1', lines: 2, total_cents: 12345 },
          ],
          skipped: [{ consumption_id: 'x', reason: 'no_price' }],
        },
      },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Addebita consumi' }));
    const summary = await screen.findByTestId('utility-run-summary');
    expect(summary).toHaveTextContent('1 addebito creato, totale');
    expect(summary).toHaveTextContent('123,45');
    expect(summary).toHaveTextContent('1 consumo rinviato');
  });
});
