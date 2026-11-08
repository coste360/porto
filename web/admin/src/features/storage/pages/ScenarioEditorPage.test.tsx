import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { MARINA_ID, makeMe, renderApp } from '../../../test/renderApp';
import { API, baseRoutes } from '../../desk/test/fixtures';
import { AREA_ID, SCENARIO_ID, plan, scenario } from '../test/fixtures';

const candidate = {
  allocation_id: 'al-yard',
  vessel: {
    id: 'v-yard',
    name: 'Grecale',
    loa_cm: 700,
    beam_cm: 250,
    hull_type: 'monohull_motor' as const,
  },
  resource_type: 'dynamic_area' as const,
  resource_id: 'area-yard',
  resource_code: 'PIAZZALE',
  first_night: '2026-11-08',
  last_night: '2027-04-30',
};

const render = (apply: { status: number; body: unknown }) =>
  renderApp(`/m/${MARINA_ID}/storage/${AREA_ID}/scenarios/${SCENARIO_ID}`, [
    ...baseRoutes(makeMe({}, ['marina.read', 'storage.read', 'storage.manage'])),
    { path: `${API}/storage/scenarios/${SCENARIO_ID}`, body: scenario },
    { path: `${API}/storage/areas/${AREA_ID}/plan`, body: plan },
    { method: 'POST', path: `${API}/storage/scenarios/${SCENARIO_ID}/apply`, ...apply },
    {
      path: `${API}/storage/scenarios/${SCENARIO_ID}/tray-candidates`,
      body: { items: [candidate] },
    },
    { method: 'POST', path: `${API}/storage/scenarios/${SCENARIO_ID}/items`, body: scenario },
    { path: `${API}/storage/movements`, body: { date: '2026-11-08', items: [], warnings: [] } },
  ]);

describe('ScenarioEditorPage', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-08T10:00:00Z'));
  });
  afterEach(() => vi.useRealTimers());

  it('applies with If-Match and lists every conflict of a 409 without changing anything', async () => {
    const { calls } = render({
      status: 409,
      body: {
        type: 'scenario_conflicts',
        title: 'conflicts',
        status: 409,
        details: {
          conflicts: [
            { allocation_id: 'al-a', reason: 'stale' },
            { allocation_id: 'al-b', reason: 'resource_taken', cells: [110] },
          ],
        },
      },
    });
    await screen.findByTestId('storage-plan');
    expect(screen.getByTestId('scenario-moves')).toHaveTextContent('Bora: spostare il 10/11/2026');
    await userEvent.click(screen.getByTestId('scenario-apply'));
    const dialog = await screen.findByRole('alertdialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Applica' }));
    const alert = await screen.findByTestId('scenario-conflicts');
    expect(alert).toHaveTextContent('Nulla è stato applicato: 2 conflitti');
    expect(alert).toHaveTextContent('Aria: cambiata dopo il caricamento');
    expect(alert).toHaveTextContent('Bora: posizione occupata');
    const call = calls.find((c) => c.url.pathname.endsWith('/apply'))!;
    expect(call.headers.get('If-Match')).toBe('"4"');
    expect(call.headers.get('Idempotency-Key')).toBeTruthy();
  });

  it('lists the tray candidates of other resources and adds one with If-Match', async () => {
    const { calls } = render({ status: 200, body: {} });
    const card = await screen.findByTestId('storage-tray-candidates');
    await within(card).findByText('Grecale');
    expect(card).toHaveTextContent('PIAZZALE, 08/11/2026 – 30/04/2027');
    await userEvent.click(
      within(card).getByRole('button', { name: "Aggiungi Grecale all'ipotesi" }),
    );
    await waitFor(() =>
      expect(calls.some((c) => c.method === 'POST' && c.url.pathname.endsWith('/items'))).toBe(
        true,
      ),
    );
    const call = calls.find((c) => c.method === 'POST' && c.url.pathname.endsWith('/items'))!;
    expect(call.headers.get('If-Match')).toBe('"4"');
    expect(call.body).toEqual({ allocation_id: 'al-yard' });
  });

  it('opens the movements after a successful apply', async () => {
    render({
      status: 200,
      body: { scenario: { ...scenario.scenario, status: 'applied' }, movements: [] },
    });
    await screen.findByTestId('storage-plan');
    await userEvent.click(screen.getByTestId('scenario-apply'));
    await userEvent.click(
      within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Applica' }),
    );
    await waitFor(() => expect(screen.getByTestId('storage-movements')).toBeInTheDocument());
  });
});
