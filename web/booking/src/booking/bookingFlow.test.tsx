import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { ApiProvider, createApiClient } from '@porto/shared/api';
import {
  createFetchMock,
  problem,
  renderWithProviders,
  type MockRoute,
} from '@porto/shared/testing';

import {
  availability,
  booking,
  config,
  created,
  emptyAvailability,
  quote,
  SLUG,
} from '../test/fixtures';
import BookingFlow from './index';
import { saveState, emptyState } from './state';

const P = `/api/public/v1/${SLUG}`;

function render(routes: MockRoute[]) {
  const mock = createFetchMock(routes);
  const api = createApiClient({ fetch: mock.fetch, baseUrl: 'http://localhost' });
  const r = renderWithProviders(
    <ApiProvider client={api}>
      <BookingFlow slug={SLUG} />
    </ApiProvider>,
  );
  return { ...r, mock };
}

const inFuture = (ms: number) => new Date(Date.now() + ms).toISOString();

async function fillSearch(user: ReturnType<typeof userEvent.setup>) {
  fireEvent.change(await screen.findByLabelText('Arrivo'), { target: { value: '2030-07-01' } });
  fireEvent.change(screen.getByLabelText('Partenza'), { target: { value: '2030-07-04' } });
  await user.type(screen.getByLabelText('Lunghezza fuori tutto'), '11,50');
  await user.type(screen.getByLabelText('Larghezza'), '3,80');
  await user.click(screen.getByRole('button', { name: 'Cerca' }));
}

describe('booking flow', () => {
  beforeEach(() => {
    window.sessionStorage.clear();
    window.history.replaceState(null, '', '/b/prova');
  });
  afterEach(() => {
    window.sessionStorage.clear();
  });

  it('walks search → berth → extras → hold with hash steps, cm data and the hold countdown', async () => {
    const user = userEvent.setup();
    const expires = inFuture(9 * 60_000);
    const { mock } = render([
      { path: `${P}/booking/config`, body: config },
      { path: `${P}/booking/availability`, body: availability },
      { method: 'POST', path: `${P}/booking/quote`, body: quote },
      { method: 'POST', path: `${P}/bookings`, status: 201, body: created(expires) },
      { path: `${P}/bookings/bk-0001-aaaa`, body: booking(expires) },
    ]);

    await fillSearch(user);
    await waitFor(() => expect(window.location.hash).toBe('#berth'));
    const search = mock.calls.find((c) => c.url.pathname.endsWith('/booking/availability'))!;
    expect(search.url.searchParams.get('loa_cm')).toBe('1150');
    expect(search.url.searchParams.get('beam_cm')).toBe('380');
    expect(await screen.findByText(/Posto 10-12 m/)).toBeInTheDocument();
    expect(screen.getByText('Non disponibile per queste date')).toBeInTheDocument();

    const cards = screen.getAllByTestId('booking-category');
    expect(cards[1]).toBeDisabled();
    await user.click(cards[0]!);
    await waitFor(() => expect(window.location.hash).toBe('#extras'));
    expect(await screen.findByTestId('price-total')).toHaveTextContent('165,00');
    const q = mock.calls.find((c) => c.url.pathname.endsWith('/booking/quote'))!;
    expect(q.body).toMatchObject({
      category: 'B',
      services: [{ item_id: 'svc-power', qty_milli: 1000 }],
    });

    await user.click(screen.getByTestId('booking-hold'));
    await waitFor(() => expect(window.location.hash).toBe('#details'));
    const create = mock.calls.find(
      (c) => c.method === 'POST' && c.url.pathname === `${P}/bookings`,
    )!;
    expect(create.headers.get('Idempotency-Key')).toBeTruthy();
    expect(create.body).toMatchObject({ arrival: '2030-07-01', category: 'B', locale: 'it' });
    expect(await screen.findByTestId('hold-countdown')).toHaveTextContent(
      /Posto bloccato per altri [89]:\d\d/,
    );
    // The token travels in a header only, never in a URL.
    const get = mock.calls.find((c) => c.url.pathname === `${P}/bookings/bk-0001-aaaa`)!;
    expect(get.headers.get('Porto-Booking-Token')).toBe('tok-secret');
    expect(mock.calls.every((c) => !c.url.href.includes('tok-secret'))).toBe(true);

    // Back goes to an earlier step through the hash.
    act(() => {
      window.location.hash = 'berth';
      window.dispatchEvent(new HashChangeEvent('hashchange'));
    });
    await waitFor(() =>
      expect(screen.getByTestId('booking-flow')).toHaveAttribute('data-step', 'berth'),
    );
  });

  it('never shows a step past the state (a deep link to #payment falls back to search)', async () => {
    window.history.replaceState(null, '', '/b/prova#payment');
    render([{ path: `${P}/booking/config`, body: config }]);
    expect(await screen.findByTestId('booking-search')).toBeInTheDocument();
    expect(window.location.hash).toBe('#search');
  });

  it('shows the empty state with the reason and the contact', async () => {
    const user = userEvent.setup();
    render([
      { path: `${P}/booking/config`, body: config },
      { path: `${P}/booking/availability`, body: { ...emptyAvailability, reason: 'min_nights' } },
    ]);
    await fillSearch(user);
    const empty = await screen.findByTestId('booking-empty');
    expect(within(empty).getByText('Il soggiorno minimo è di 1 notti.')).toBeInTheDocument();
    expect(within(empty).getByRole('link', { name: /Chiama il marina/ })).toHaveAttribute(
      'href',
      'tel:+39010000000',
    );
  });

  it('recovers from hold_expired: search again with the same input', async () => {
    const user = userEvent.setup();
    saveState(SLUG, {
      ...emptyState,
      search: { arrival: '2030-07-01', departure: '2030-07-04', loa_cm: 1150, beam_cm: 380 },
      category: 'B',
      booking: { id: 'bk-0001-aaaa', token: 'tok-secret', reference: 'K7M2Q9XA' },
    });
    window.history.replaceState(null, '', '/b/prova#details');
    render([
      { path: `${P}/booking/config`, body: config },
      { path: `${P}/booking/availability`, body: availability },
      { path: `${P}/bookings/bk-0001-aaaa`, body: booking(inFuture(-1000), { status: 'expired' }) },
    ]);
    const alert = await screen.findByTestId('hold-expired');
    expect(within(alert).getByText('Il blocco del posto è scaduto')).toBeInTheDocument();
    await user.click(within(alert).getByRole('button', { name: 'Cerca di nuovo' }));
    await waitFor(() => expect(window.location.hash).toBe('#berth'));
    expect(await screen.findAllByTestId('booking-category')).toHaveLength(2);
    expect(screen.queryByTestId('hold-expired')).toBeNull();
  });

  it('marks the hold expired when the countdown reaches zero', async () => {
    const expires = inFuture(1500);
    saveState(SLUG, {
      ...emptyState,
      search: { arrival: '2030-07-01', departure: '2030-07-04', loa_cm: 1150, beam_cm: 380 },
      category: 'B',
      booking: { id: 'bk-0001-aaaa', token: 'tok-secret', reference: 'K7M2Q9XA' },
    });
    window.history.replaceState(null, '', '/b/prova#details');
    render([
      { path: `${P}/booking/config`, body: config },
      { path: `${P}/bookings/bk-0001-aaaa`, body: booking(expires) },
    ]);
    expect(await screen.findByTestId('hold-countdown')).toBeInTheDocument();
    expect(await screen.findByTestId('hold-expired', {}, { timeout: 4000 })).toBeInTheDocument();
  });

  it('goes back to the berth list when the category was taken meanwhile', async () => {
    const user = userEvent.setup();
    saveState(SLUG, {
      ...emptyState,
      search: { arrival: '2030-07-01', departure: '2030-07-04', loa_cm: 1150, beam_cm: 380 },
      category: 'B',
    });
    window.history.replaceState(null, '', '/b/prova#extras');
    render([
      { path: `${P}/booking/config`, body: config },
      { path: `${P}/booking/availability`, body: availability },
      { method: 'POST', path: `${P}/booking/quote`, body: quote },
      {
        method: 'POST',
        path: `${P}/bookings`,
        status: 409,
        body: problem('booking_unavailable', 409, { details: { available_categories: [] } }),
      },
    ]);
    await screen.findByTestId('price-total');
    await user.click(screen.getByTestId('booking-hold'));
    await waitFor(() => expect(window.location.hash).toBe('#berth'));
    expect(await screen.findByText(/appena stato occupato/)).toBeInTheDocument();
  });

  it('shows the confirmation of a booking already confirmed', async () => {
    saveState(SLUG, {
      ...emptyState,
      booking: { id: 'bk-0001-aaaa', token: 'tok-secret', reference: 'K7M2Q9XA' },
    });
    render([
      { path: `${P}/booking/config`, body: config },
      { path: `${P}/bookings/bk-0001-aaaa`, body: booking(inFuture(0), { status: 'confirmed' }) },
    ]);
    const outcome = await screen.findByTestId('booking-outcome');
    expect(outcome).toHaveAttribute('data-outcome', 'confirmed');
    expect(within(outcome).getByText('Prenotazione confermata')).toBeInTheDocument();
    expect(within(outcome).getByTestId('booking-reference')).toHaveTextContent('K7M2Q9XA');
    expect(within(outcome).getByText('Canale VHF 9')).toBeInTheDocument();
  });
});
