import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import type { OnlineBookingDetail, OnlineBookingSummary } from '@porto/shared/api';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../test/renderApp';
import { pickOption } from '../pricing/test/dom';
import { remaining } from './lib/constants';

const M = `/api/admin/v1/marinas/${MARINA_ID}`;
const ID = '0190a000-0000-7000-8000-0000000000c1';
const deadline = new Date(Date.now() + 3 * 3600_000 + 5 * 60_000).toISOString();

const summary: OnlineBookingSummary = {
  contract_id: ID,
  reference: 'K7M2Q9XA',
  code: 'T-2026-0001',
  status: 'awaiting_approval',
  channel: 'online',
  arrival: '2026-12-01',
  departure: '2026-12-04',
  loa_cm: 2200,
  beam_cm: 600,
  approval_reason: 'loa',
  approval_deadline: deadline,
  total_cents: 45000,
  created_at: '2026-11-07T10:00:00Z',
  version: 3,
};

const detail: OnlineBookingDetail = {
  ...summary,
  transit: { id: ID, code: 'T-2026-0001', kind: 'transit', status: 'awaiting_approval' },
  contact: {
    first_name: 'Anna',
    last_name: 'Bianchi',
    email: 'anna@example.test',
    country: 'IT',
    locale: 'it',
  },
  documents: [
    {
      id: 'doc-1',
      type: 'insurance_policy',
      file_name: 'polizza.pdf',
      uploaded_at: '2026-11-07T10:05:00Z',
    },
  ],
  consents: [{ kind: 'terms', version: '2', granted: true, recorded_at: '2026-11-07T10:04:00Z' }],
  payment: { state: 'authorized', amount_cents: 45000, capture_method: 'manual' },
};

const desk = makeMe({}, ['marina.read', 'transits.desk', 'customers.read', 'customers.write']);

describe('OnlineBookingsPage', () => {
  it('formats the remaining time of a deadline', () => {
    const now = Date.parse('2026-11-08T10:00:00Z');
    expect(remaining('2026-11-08T12:05:30Z', now)).toBe('2 h 05 min');
    expect(remaining('2026-11-08T10:12:00Z', now)).toBe('12 min');
    expect(remaining('2026-11-08T09:00:00Z', now)).toBe('');
  });

  it('lists bookings, opens the sheet, verifies a document and approves with If-Match', async () => {
    const user = userEvent.setup();
    const { calls } = renderApp(`/m/${MARINA_ID}/online`, [
      meRoute(desk),
      { path: `${M}/online-bookings`, body: { items: [summary], next_cursor: null } },
      { path: `${M}/online-bookings/${ID}`, body: detail },
      {
        method: 'POST',
        path: `${M}/documents/doc-1/verify`,
        body: { document_id: 'doc-1', verified: true },
      },
      {
        method: 'POST',
        path: `${M}/online-bookings/${ID}/approve`,
        body: { ...detail, status: 'pending_payment' },
      },
    ]);
    await user.click(await screen.findByTestId('online-booking-open'));
    const sheet = await screen.findByTestId('online-booking-sheet');
    expect(await within(sheet).findByTestId('online-deadline')).toHaveTextContent('3 h 0');
    expect(await within(sheet).findByText('anna@example.test')).toBeInTheDocument();
    expect(within(sheet).getByText('Lunghezza oltre la soglia')).toBeInTheDocument();
    await user.click(within(sheet).getByTestId('online-document-verify'));
    await waitFor(() =>
      expect(calls.find((c) => c.url.pathname === `${M}/documents/doc-1/verify`)?.body).toEqual({
        verified: true,
      }),
    );
    await user.click(within(sheet).getByTestId('online-approve'));
    await waitFor(() => {
      const approve = calls.find((c) => c.url.pathname.endsWith('/approve'));
      expect(approve?.headers.get('If-Match')).toBe('"3"');
    });
  });

  it('rejects with a reason and the notify flag', async () => {
    const user = userEvent.setup();
    const { calls } = renderApp(`/m/${MARINA_ID}/online`, [
      meRoute(desk),
      { path: `${M}/online-bookings`, body: { items: [summary], next_cursor: null } },
      { path: `${M}/online-bookings/${ID}`, body: detail },
      {
        method: 'POST',
        path: `${M}/online-bookings/${ID}/reject`,
        body: { ...detail, status: 'cancelled' },
      },
    ]);
    await user.click(await screen.findByTestId('online-booking-open'));
    await user.click(await screen.findByTestId('online-reject'));
    const confirm = await screen.findByTestId('online-reject-confirm');
    expect(confirm).toBeDisabled();
    await user.type(screen.getByLabelText('Motivo'), 'Barca troppo grande');
    await user.click(confirm);
    await waitFor(() => {
      const reject = calls.find((c) => c.url.pathname.endsWith('/reject'));
      expect(reject?.body).toEqual({ reason: 'Barca troppo grande', notify: true });
    });
  });

  it('filters by status', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/online`, [
      meRoute(desk),
      { path: `${M}/online-bookings`, body: { items: [], next_cursor: null } },
    ]);
    expect(await screen.findByText('Nessuna prenotazione online.')).toBeInTheDocument();
    await pickOption('Stato', 'Da approvare');
    await waitFor(() =>
      expect(calls.some((c) => c.url.searchParams.get('status') === 'awaiting_approval')).toBe(
        true,
      ),
    );
  });
});
