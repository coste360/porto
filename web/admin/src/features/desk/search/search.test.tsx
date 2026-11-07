import { createRoute } from '@tanstack/react-router';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { problem } from '@porto/shared/testing';

import { MARINA_ID, renderApp } from '../../../test/renderApp';
import '../../pricing/test/dom';
import { API, B1, B2, TRANSIT_ID, baseRoutes, solution, transit } from '../test/fixtures';
import { IncomingVesselPanel } from './IncomingVesselPanel';

const onSolutions = vi.fn();
const onFocusSolution = vi.fn();

// The operational view (WEB-OPSMAP) hosts the panel; here a bare route renders it alone.
vi.mock('../../operations/routes', () => ({
  operationsRoutes: (parent: never) => [
    createRoute({
      getParentRoute: () => parent,
      path: 'ops',
      component: () => (
        <IncomingVesselPanel
          marinaId={MARINA_ID}
          date="2026-07-10"
          onSolutions={onSolutions}
          onFocusSolution={onFocusSolution}
        />
      ),
    }),
  ],
}));
vi.mock('../../operations/public', () => ({
  opsMapSearch: () => ({}),
  PlacementPicker: () => null,
}));
vi.mock('../../maps/routes', () => ({ mapRoutes: () => [] }));

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-07-10T08:00:00Z'));
  onSolutions.mockReset();
  onFocusSolution.mockReset();
});
afterEach(() => vi.useRealTimers());

const searchResult = {
  solutions: [solution('k1', 'A01', B1, 12000), solution('k2', 'A02', B2, 15000)],
  considered: 40,
};

async function runSearch(calls: { method: string; url: URL; body: unknown }[]) {
  await screen.findByRole('heading', { name: 'Imbarcazione in ingresso' });
  await userEvent.type(screen.getByLabelText('Lunghezza fuori tutto'), '11,34');
  await userEvent.type(screen.getByLabelText('Larghezza'), '3,8');
  // Stay: arrival today (default), departure today + 1 → set 3 nights through the Date pickers is heavy;
  // the default 1-night stay is enough for the mapping.
  await userEvent.click(screen.getByLabelText('Considera liberi i posti con la barca fuori'));
  await userEvent.click(screen.getByRole('button', { name: 'Cerca' }));
  await waitFor(() =>
    expect(calls.some((c) => c.url.pathname.endsWith('/occupancies/search'))).toBe(true),
  );
}

describe('IncomingVesselPanel', () => {
  it('maps the form to an integer search request and reports the solutions', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/ops`, [
      ...baseRoutes(),
      { method: 'POST', path: `${API}/occupancies/search`, body: searchResult },
    ]);
    await runSearch(calls);
    expect(calls.find((c) => c.url.pathname.endsWith('/occupancies/search'))!.body).toEqual({
      purpose: 'transit',
      arrival: '2026-07-10',
      departure: '2026-07-11',
      resource_types: ['berth', 'dynamic_area'],
      channel: 'desk',
      vessel: { loa_cm: 1134, beam_cm: 380, hull_type: 'monohull_motor' },
      consider_out: true,
    });
    const items = await screen.findAllByTestId('desk-solution');
    expect(items).toHaveLength(2);
    expect(onSolutions).toHaveBeenCalledWith(searchResult.solutions);
    expect(within(items[0]!).getByText('120,00 €')).toBeInTheDocument();
    fireEvent.mouseEnter(items[1]!);
    expect(onFocusSolution).toHaveBeenLastCalledWith(searchResult.solutions[1]);
    fireEvent.mouseLeave(items[1]!);
    expect(onFocusSolution).toHaveBeenLastCalledWith(null);
  });

  it('saves an empty search as an unserved request', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/ops`, [
      ...baseRoutes(),
      {
        method: 'POST',
        path: `${API}/occupancies/search`,
        body: { solutions: [], considered: 40 },
      },
      { method: 'POST', path: `${API}/inquiries`, status: 201, body: { id: 'i1' } },
    ]);
    await runSearch(calls);
    await userEvent.click(
      await screen.findByRole('button', { name: 'Salva come richiesta non evasa' }),
    );
    await waitFor(() =>
      expect(calls.some((c) => c.url.pathname.endsWith('/inquiries'))).toBe(true),
    );
    expect(calls.find((c) => c.url.pathname.endsWith('/inquiries'))!.body).toEqual({
      purpose: 'transit',
      source: 'desk',
      channel: 'desk',
      arrival: '2026-07-10',
      departure: '2026-07-11',
      outcome: 'declined_no_space',
      loa_cm: 1134,
      beam_cm: 380,
    });
    expect(await screen.findByRole('button', { name: 'Richiesta salvata' })).toBeDisabled();
  });

  it('runs the transit wizard from a solution: new vessel, hold, then an alternative after a conflict', async () => {
    let attempt = 0;
    const { calls } = renderApp(`/m/${MARINA_ID}/ops`, [
      ...baseRoutes(),
      { method: 'POST', path: `${API}/occupancies/search`, body: searchResult },
      {
        method: 'POST',
        path: (url) => url.pathname === `${API}/transits` && attempt++ === 0,
        status: 409,
        body: problem('resource_taken', 409, {
          details: {
            resource_type: 'berth',
            resource_id: B1,
            from: '2026-07-10',
            to: '2026-07-10',
            allocation_ids: ['x'],
            alternatives: [solution('alt', 'A02', B2, 15000)],
          },
        }),
      },
      { method: 'POST', path: `${API}/transits`, status: 201, body: transit() },
    ]);
    await runSearch(calls);
    const [first] = await screen.findAllByTestId('desk-solution');
    await userEvent.click(within(first!).getByRole('button', { name: 'Nuovo transito' }));
    const dialog = await screen.findByTestId('desk-transit-dialog');
    // Step 1: a new vessel with estimated dimensions; Continue stays disabled until it is complete.
    const next = within(dialog).getByRole('button', { name: 'Continua' });
    expect(next).toBeDisabled();
    await userEvent.click(within(dialog).getByRole('radio', { name: 'Nuova' }));
    await userEvent.type(within(dialog).getByLabelText("Nome dell'imbarcazione"), 'Maestrale');
    await userEvent.type(within(dialog).getByLabelText('Lunghezza fuori tutto'), '11,34');
    await userEvent.type(within(dialog).getByLabelText('Larghezza'), '3,80');
    await userEvent.click(next);
    // Step 2: the place comes from the search.
    expect(within(dialog).getByTestId('desk-transit-solution')).toHaveTextContent('A01');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Continua' }));
    // Step 3: manual confirmation needs a customer, so without one only the hold is possible.
    expect(within(dialog).getByTestId('desk-transit-price')).toHaveTextContent('120,00 €');
    await userEvent.click(within(dialog).getByLabelText('Conferma e registra pagamento manuale'));
    expect(within(dialog).getByRole('button', { name: 'Conferma il transito' })).toBeDisabled();
    await userEvent.click(within(dialog).getByLabelText('Conferma e registra pagamento manuale'));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Tieni in opzione' }));
    // Conflict: the alternative is one click away and re-submits on the other berth.
    await userEvent.click(await within(dialog).findByRole('button', { name: /A02/ }));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Tieni in opzione' }));
    expect(await within(dialog).findByText('Transito TRN-2026-0001 creato')).toBeInTheDocument();
    const posts = calls.filter((c) => c.method === 'POST' && c.url.pathname === `${API}/transits`);
    expect(posts).toHaveLength(2);
    expect(posts[0]!.body).toEqual({
      arrival: '2026-07-10',
      departure: '2026-07-11',
      channel: 'desk',
      new_vessel: { name: 'Maestrale', loa_cm: 1134, beam_cm: 380, hull_type: 'monohull_motor' },
      placements: [
        {
          resource_type: 'berth',
          resource_id: B1,
          first_night: '2026-07-10',
          last_night: '2026-07-12',
        },
      ],
    });
    expect(
      (posts[1]!.body as { placements: { resource_id: string }[] }).placements[0]!.resource_id,
    ).toBe(B2);
    expect(posts[0]!.headers.get('Idempotency-Key')).not.toBe(
      posts[1]!.headers.get('Idempotency-Key'),
    );
    expect(within(dialog).getByRole('link', { name: 'Apri il transito' })).toHaveAttribute(
      'href',
      `/admin/m/${MARINA_ID}/transits/${TRANSIT_ID}`,
    );
  });
});
