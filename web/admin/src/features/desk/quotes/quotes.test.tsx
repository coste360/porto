import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Schemas } from '@porto/shared/api';

import { MARINA_ID, renderApp } from '../../../test/renderApp';
import { pickOption } from '../../pricing/test/dom';
import {
  draftFromQuote,
  emptyOption,
  expiryCountdown,
  optionFromSolution,
  quoteInput,
} from '../lib/quote';
import {
  API,
  B1,
  B2,
  CONTRACT_ID,
  CUSTOMER_ID,
  QUOTE_ID,
  VESSEL_ID,
  baseRoutes,
  solution,
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

const quote = (o: Partial<Schemas['Quote']> = {}): Schemas['Quote'] => ({
  id: QUOTE_ID,
  code: 'PRV-2026-0001',
  number: 1,
  year: 2026,
  status: 'sent',
  kind: 'transit',
  customer: { id: CUSTOMER_ID, display_name: 'Mario Rossi' },
  vessel: vesselBrief,
  starts_on: '2026-07-20',
  ends_on: '2026-07-23',
  valid_until: '2026-07-13',
  expires_at: '2026-07-13T21:59:59Z',
  channel: 'desk',
  options: [
    {
      id: 'o1',
      seq: 1,
      label: 'Pontile A',
      hold: true,
      placements: [
        {
          seq: 1,
          resource_type: 'berth',
          resource_id: B1,
          resource_code: 'A01',
          first_night: '2026-07-20',
          last_night: '2026-07-22',
          allocation_id: 'h1',
        },
      ],
      total_cents: 30000,
      deposit_cents: 0,
      engine_version: 1,
    },
    {
      id: 'o2',
      seq: 2,
      label: '',
      hold: true,
      placements: [
        {
          seq: 1,
          resource_type: 'berth',
          resource_id: B2,
          resource_code: 'A02',
          first_night: '2026-07-20',
          last_night: '2026-07-21',
        },
        {
          seq: 2,
          resource_type: 'berth',
          resource_id: B1,
          resource_code: 'A01',
          first_night: '2026-07-22',
          last_night: '2026-07-22',
        },
      ],
      total_cents: 28000,
      deposit_cents: 5000,
      engine_version: 1,
    },
  ],
  created_at: '2026-07-10T08:00:00Z',
  version: 2,
  ...o,
});

describe('quote draft', () => {
  const base = {
    kind: 'transit' as const,
    term: 'annual' as const,
    customerId: CUSTOMER_ID,
    vesselId: VESSEL_ID,
    startsOn: '2026-07-20',
    endsOn: '2026-07-23',
    validUntil: null,
    notes: '',
  };

  it('builds options with holds and two segments split on a night', () => {
    const r = quoteInput({
      ...base,
      options: [
        {
          label: ' Uno ',
          hold: true,
          targets: [{ resource_type: 'berth', resource_id: B1 }],
          splitOn: null,
        },
        {
          label: '',
          hold: false,
          targets: [
            { resource_type: 'berth', resource_id: B2 },
            { resource_type: 'dynamic_area', resource_id: 'a', x_cm: 1200 },
          ],
          splitOn: '2026-07-22',
        },
      ],
    });
    expect(r.ok && r.body.options).toEqual([
      {
        hold: true,
        label: 'Uno',
        placements: [
          {
            resource_type: 'berth',
            resource_id: B1,
            first_night: '2026-07-20',
            last_night: '2026-07-22',
          },
        ],
      },
      {
        hold: false,
        placements: [
          {
            resource_type: 'berth',
            resource_id: B2,
            first_night: '2026-07-20',
            last_night: '2026-07-21',
          },
          {
            resource_type: 'dynamic_area',
            resource_id: 'a',
            x_cm: 1200,
            first_night: '2026-07-22',
            last_night: '2026-07-22',
          },
        ],
      },
    ]);
    expect(r.ok && r.body.term).toBeUndefined();
  });

  it('refuses incomplete drafts with the first problem', () => {
    expect(quoteInput({ ...base, customerId: null, options: [emptyOption()] })).toEqual({
      ok: false,
      error: 'desk:quote.errors.customer',
    });
    expect(quoteInput({ ...base, options: [emptyOption()] })).toEqual({
      ok: false,
      error: 'desk:quote.errors.placement',
    });
    expect(
      quoteInput({
        ...base,
        options: [
          {
            label: '',
            hold: false,
            targets: [
              { resource_type: 'berth', resource_id: B1 },
              { resource_type: 'berth', resource_id: B2 },
            ],
            splitOn: '2026-07-20',
          },
        ],
      }),
    ).toEqual({ ok: false, error: 'desk:quote.errors.split' });
    expect(quoteInput({ ...base, options: [] })).toEqual({
      ok: false,
      error: 'desk:quote.errors.options',
    });
    expect(quoteInput({ ...base, endsOn: '2026-07-20', options: [emptyOption()] })).toEqual({
      ok: false,
      error: 'desk:quote.errors.dates',
    });
  });

  it('round-trips a stored quote and takes options from solutions', () => {
    const d = draftFromQuote(quote());
    expect(d.options[1]).toEqual({
      label: '',
      hold: true,
      targets: [
        { resource_type: 'berth', resource_id: B2 },
        { resource_type: 'berth', resource_id: B1 },
      ],
      splitOn: '2026-07-22',
    });
    expect(quoteInput(d).ok).toBe(true);
    expect(optionFromSolution(solution('k', 'A01', B1))).toEqual({
      label: '',
      hold: true,
      targets: [{ resource_type: 'berth', resource_id: B1 }],
      splitOn: null,
    });
  });

  it('counts down to the expiry in whole days and hours', () => {
    expect(expiryCountdown('2026-07-13T21:59:59Z', new Date('2026-07-10T08:00:00Z'))).toEqual({
      days: 3,
      hours: 13,
    });
    expect(expiryCountdown('2026-07-10T07:00:00Z', new Date('2026-07-10T08:00:00Z'))).toBeNull();
  });
});

describe('quote pages', () => {
  it('creates a quote with a held option and a second option picked from the list', async () => {
    const { calls } = renderApp(
      `/m/${MARINA_ID}/quotes/new?vessel=${VESSEL_ID}&rt=berth&rid=${B1}&from=2026-07-20&to=2026-07-23`,
      [
        ...baseRoutes(),
        { method: 'POST', path: `${API}/quotes`, status: 201, body: quote({ status: 'draft' }) },
        { path: `${API}/quotes/${QUOTE_ID}`, body: quote({ status: 'draft' }) },
      ],
    );
    const editor = await screen.findByTestId('desk-quote-editor');
    await pickOption('Cliente', 'Mario Rossi', editor);
    const options = within(editor).getAllByTestId('desk-quote-option');
    await userEvent.click(within(options[0]!).getByRole('switch'));
    await userEvent.click(within(editor).getByRole('button', { name: "Aggiungi un'alternativa" }));
    const second = within(editor).getAllByTestId('desk-quote-option')[1]!;
    await pickOption('Posto o risorsa', /A02/, second);
    await userEvent.click(within(editor).getByRole('button', { name: 'Crea il preventivo' }));
    await waitFor(() =>
      expect(calls.some((c) => c.method === 'POST' && c.url.pathname === `${API}/quotes`)).toBe(
        true,
      ),
    );
    expect(
      calls.find((c) => c.method === 'POST' && c.url.pathname === `${API}/quotes`)!.body,
    ).toEqual({
      kind: 'transit',
      customer_id: CUSTOMER_ID,
      vessel_id: VESSEL_ID,
      starts_on: '2026-07-20',
      ends_on: '2026-07-23',
      channel: 'desk',
      options: [
        {
          hold: true,
          placements: [
            {
              resource_type: 'berth',
              resource_id: B1,
              first_night: '2026-07-20',
              last_night: '2026-07-22',
            },
          ],
        },
        {
          hold: false,
          placements: [
            {
              resource_type: 'berth',
              resource_id: B2,
              first_night: '2026-07-20',
              last_night: '2026-07-22',
            },
          ],
        },
      ],
    });
    expect(await screen.findByRole('heading', { name: 'PRV-2026-0001' })).toBeInTheDocument();
  });

  it('shows options, holds and the expiry, and accepts option 2 with If-Match', async () => {
    const { calls, router } = renderApp(`/m/${MARINA_ID}/quotes/${QUOTE_ID}`, [
      ...baseRoutes(),
      { path: `${API}/quotes/${QUOTE_ID}`, body: quote() },
      {
        method: 'POST',
        path: `${API}/quotes/${QUOTE_ID}/accept`,
        body: { quote: quote({ status: 'accepted' }), contract: transit({ id: CONTRACT_ID }) },
      },
      { path: `${API}/contracts/${CONTRACT_ID}`, body: transit({ id: CONTRACT_ID }) },
    ]);
    const cards = await screen.findAllByTestId('desk-quote-option-card');
    expect(within(cards[0]!).getByText('Posto in opzione')).toBeInTheDocument();
    expect(within(cards[1]!).getByText('Opzione non più attiva')).toBeInTheDocument();
    expect(within(cards[1]!).getByText('Acconto 50,00 €')).toBeInTheDocument();
    expect(screen.getByTestId('desk-quote-expiry')).toHaveTextContent('Scade tra 3 g e 13 h');
    expect(screen.queryByRole('button', { name: 'Modifica' })).toBeNull();
    await userEvent.click(screen.getByRole('button', { name: 'Accetta' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('radio', { name: /Alternativa 2/ }));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Accetta' }));
    await waitFor(() =>
      expect(router.state.location.pathname).toBe(`/m/${MARINA_ID}/transits/${CONTRACT_ID}`),
    );
    const accept = calls.find((c) => c.url.pathname.endsWith('/accept'))!;
    expect(accept.headers.get('If-Match')).toBe('"2"');
    expect(accept.body).toEqual({ option_id: 'o2' });
  });
});
