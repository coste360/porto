import { fireEvent, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { MARINA_ID, makeMe, renderApp } from '../../../test/renderApp';
import { API, baseRoutes } from '../../desk/test/fixtures';

const B = '01900000-0000-7000-8000-0000000000b1';
const entry = {
  booking_id: B,
  number: 'NL2027-000012',
  status: 'confirmed',
  unit_id: 'u1',
  unit_code: 'GOM58',
  renter_name: 'Ada Rossi',
  start: { date: '2027-06-12', slot: 16 },
  end: { date: '2027-06-12', slot: 26 },
  starts_at: '2027-06-12T06:00:00Z',
  ends_at: '2027-06-12T11:00:00Z',
  late: false,
  deposit_status: 'none',
  unpaid_cents: 16500,
};
const booking = {
  id: B,
  number: 'NL2027-000012',
  status: 'confirmed',
  channel: 'desk',
  unit_id: 'u1',
  unit_code: 'GOM58',
  formula_id: 'f1',
  formula_code: 'MEZZA-MATTINA',
  kind: 'half_day',
  start: entry.start,
  end: entry.end,
  starts_at: entry.starts_at,
  ends_at: entry.ends_at,
  slots_lost: false,
  invoice_requested: false,
  fiscal_mode: 'receipt',
  licence_required: true,
  allow_outside_hours: false,
  total_cents: 16500,
  engine_version: 1,
  lines: [],
  extras: [],
  deposit: { status: 'none', amount_cents: 50000, retained_cents: 0 },
  charges: [],
  note: '',
  version: 3,
  created_at: '2027-06-01T08:00:00Z',
  updated_at: '2027-06-01T08:00:00Z',
};
const handover = {
  booking_id: B,
  phase: 'checkout',
  checklist: [],
  notes: '',
  files: [],
  missing: [
    'identity',
    'licence',
    'contract',
    'deposit',
    'checklist',
    'fuel',
    'engine_hours',
    'photos',
  ],
  version: 0,
};

describe('RentalPhonePage', () => {
  it('lists today and opens the check-out wizard on the first missing step, licence included', async () => {
    renderApp(`/m/${MARINA_ID}/rental-phone`, [
      ...baseRoutes(
        makeMe({}, ['marina.read', 'rental.manage', 'payments.collect', 'payments.collect_cash']),
      ),
      {
        path: `${API}/rental/day`,
        body: { date: '2027-06-12', pickups: [entry], out: [], returns_due: [], late: [] },
      },
      { path: `${API}/rental/bookings/${B}`, body: booking },
      { path: `${API}/rental/bookings/${B}/handovers/checkout`, body: handover },
      { path: `${API}/rental/units`, body: { items: [], next_cursor: null } },
    ]);
    const item = await screen.findByTestId('rental-day-entry');
    expect(item).toHaveTextContent('GOM58');
    expect(
      within(await screen.findByTestId('rental-day-pickups')).getByText('Partenze'),
    ).toBeInTheDocument();
    await userEvent.click(item);
    const wizard = await screen.findByTestId('checkout-wizard');
    expect(await within(wizard).findByTestId('identity-step')).toBeInTheDocument();
    expect(within(wizard).getByText('Patente')).toBeInTheDocument();
    expect(within(wizard).getByTestId('identity-save')).toBeDisabled();
    await userEvent.type(within(wizard).getByLabelText('Numero del documento'), 'CA12345XY');
    await userEvent.type(within(wizard).getByLabelText('Scadenza del documento'), '2030-01-01');
    expect(within(wizard).getByTestId('identity-save')).toBeEnabled();
  });

  it('books a walk-in from now (Nuovo) and opens the check-out wizard', async () => {
    Element.prototype.scrollIntoView = vi.fn();
    Element.prototype.hasPointerCapture = vi.fn(() => false);
    vi.useFakeTimers({ toFake: ['Date'] });
    // 08:40 UTC = 10:40 in Rome: slot 21; the window closes at 19:00 (slot 38).
    vi.setSystemTime(new Date('2027-06-12T08:40:00Z'));
    const { calls } = renderApp(`/m/${MARINA_ID}/rental-phone`, [
      ...baseRoutes(makeMe({}, ['marina.read', 'rental.manage', 'payments.collect'])),
      {
        path: `${API}/rental/day`,
        body: { date: '2027-06-12', pickups: [], out: [], returns_due: [], late: [] },
      },
      {
        path: `${API}`,
        body: { id: MARINA_ID, timezone: 'Europe/Rome', name: 'Porto' },
      },
      {
        path: `${API}/rental/settings`,
        body: { open_from_slot: 16, open_to_slot: 38, bands: [], checklist: [], version: 1 },
      },
      {
        path: `${API}/rental/units`,
        body: {
          items: [
            { id: 'u1', code: 'GOM58', vessel_name: 'Gabbiano', status: 'available' },
            { id: 'u2', code: 'SUP1', vessel_name: 'Tavola', status: 'maintenance' },
          ],
          next_cursor: null,
        },
      },
      {
        method: 'POST',
        path: `${API}/rental/quote`,
        body: {
          items: [
            { formula_id: 'f1', formula_code: 'ORARIA', ok: true, total_cents: 9000 },
            { formula_id: 'f2', formula_code: 'GIORNATA', ok: false, total_cents: 0 },
          ],
        },
      },
      { method: 'POST', path: `${API}/rental/bookings`, status: 201, body: booking },
      { path: `${API}/rental/bookings/${B}`, body: booking },
      { path: `${API}/rental/bookings/${B}/handovers/checkout`, body: handover },
    ]);
    await userEvent.click(await screen.findByRole('tab', { name: 'Nuovo' }));
    const tab = await screen.findByTestId('rental-walk-in');
    fireEvent.keyDown(within(tab).getByRole('combobox', { name: 'Unità' }), { key: 'Enter' });
    expect(screen.queryByRole('option', { name: /SUP1/ })).toBeNull();
    await userEvent.click(await screen.findByRole('option', { name: /GOM58/ }));
    fireEvent.keyDown(within(tab).getByRole('combobox', { name: 'Durata' }), { key: 'Enter' });
    // From 10:30 the window (until 19:00) leaves 8 whole hours.
    expect(await screen.findByRole('option', { name: '8 ore' })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: '9 ore' })).toBeNull();
    await userEvent.click(await screen.findByRole('option', { name: '2 ore' }));
    expect(await within(tab).findByText('10:30-12:30')).toBeInTheDocument();
    expect(await within(tab).findByLabelText(/ORARIA/)).toBeChecked();
    await userEvent.type(within(tab).getByLabelText('Nome del cliente'), 'Ada Rossi');
    await userEvent.click(within(tab).getByTestId('walk-in-confirm'));
    expect(await screen.findByTestId('checkout-wizard')).toBeInTheDocument();
    const post = calls.find(
      (c) => c.method === 'POST' && c.url.pathname.endsWith('/rental/bookings'),
    );
    expect(post?.body).toMatchObject({
      unit_id: 'u1',
      formula_id: 'f1',
      start: { date: '2027-06-12', slot: 21 },
      end: { date: '2027-06-12', slot: 25 },
      hold: false,
      renter: { name: 'Ada Rossi' },
    });
  });

  it('shows the payment link as a QR and disables it for a walk-in renter', async () => {
    renderApp(`/m/${MARINA_ID}/rental-phone`, [
      ...baseRoutes(makeMe({}, ['marina.read', 'rental.manage', 'payments.collect'])),
      {
        path: `${API}/rental/day`,
        body: {
          date: '2027-06-12',
          pickups: [{ ...entry, status: 'closed' }],
          out: [],
          returns_due: [],
          late: [],
        },
      },
      { path: `${API}/rental/bookings/${B}`, body: booking },
    ]);
    await userEvent.click(await screen.findByTestId('rental-day-entry'));
    const dialog = await screen.findByTestId('quick-cash-dialog');
    const link = within(dialog).getByRole('button', { name: 'Link di pagamento' });
    await vi.waitFor(() => expect(link).toBeDisabled());
    expect(within(dialog).getByText(/richiede un cliente in anagrafica/)).toBeInTheDocument();
  });
});

describe('RentalPhonePage quick-cash link', () => {
  it('renders the link QR for a registered customer', async () => {
    const withCustomer = { ...booking, customer_id: 'c1' };
    renderApp(`/m/${MARINA_ID}/rental-phone`, [
      ...baseRoutes(makeMe({}, ['marina.read', 'rental.manage', 'payments.collect'])),
      {
        path: `${API}/rental/day`,
        body: {
          date: '2027-06-12',
          pickups: [{ ...entry, status: 'closed' }],
          out: [],
          returns_due: [],
          late: [],
        },
      },
      { path: `${API}/rental/bookings/${B}`, body: withCustomer },
      {
        method: 'POST',
        path: `${API}/rental/bookings/${B}/quick-cash`,
        body: {
          charges: [],
          fiscal_document_ids: [],
          link_url: 'https://porto.test/b/x/pay/tok',
          link_qr_svg: '<svg xmlns="http://www.w3.org/2000/svg"></svg>',
        },
      },
    ]);
    await userEvent.click(await screen.findByTestId('rental-day-entry'));
    const dialog = await screen.findByTestId('quick-cash-dialog');
    const link = within(dialog).getByRole('button', { name: 'Link di pagamento' });
    await vi.waitFor(() => expect(link).toBeEnabled());
    await userEvent.click(link);
    const qr = await within(dialog).findByTestId('quick-cash-qr');
    expect(qr).toHaveAttribute('src', expect.stringMatching(/^data:image\/svg\+xml/));
    expect(within(dialog).getByTestId('quick-cash-link')).toHaveAttribute(
      'href',
      'https://porto.test/b/x/pay/tok',
    );
  });
});

afterEach(() => vi.useRealTimers());
