import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import { ApiProvider, createApiClient, type AccountBooking } from '@porto/shared/api';
import {
  createFetchMock,
  problem,
  renderWithProviders,
  type MockRoute,
} from '@porto/shared/testing';

import { fileProblem } from './documents';
import { AccountPage } from './index';
import { writeSession } from './session';

const SLUG = 'marina-test';
const P = `/api/public/v1/${SLUG}`;
const ID = '01900000-0000-7000-8000-000000000001';
const SESSION = 'S'.repeat(52);

const booking: AccountBooking = {
  contract_id: ID,
  code: 'T-2026-0001',
  reference: 'K7M2Q9XA',
  status: 'confirmed',
  channel: 'online',
  arrival: '2026-12-01',
  departure: '2026-12-04',
  vessel_name: 'Libeccio',
  total_cents: 30000,
  paid_cents: 9000,
  residual_cents: 21000,
  approval_pending: false,
  berth_code: 'A-12',
  qr_payload: `PT1:${ID}`,
  can_cancel: true,
  can_change_dates: true,
  can_pay_balance: true,
  documents: [
    { id: 'doc-1', type: 'insurance_policy', uploaded_at: '2026-11-07T10:00:00Z', verified: true },
  ],
  missing_documents: ['boating_licence'],
  receipts: [
    { payment_id: 'p1', received_on: '2026-11-07', amount_cents: 9000, method: 'card_online' },
  ],
};

function render(routes: MockRoute[]) {
  writeSession(SLUG, { token: SESSION, expiresAt: '2999-01-01T00:00:00Z' });
  const mock = createFetchMock(routes);
  const api = createApiClient({ fetch: mock.fetch, baseUrl: 'http://localhost' });
  renderWithProviders(
    <ApiProvider client={api}>
      <AccountPage slug={SLUG} contractId={ID} />
    </ApiProvider>,
  );
  return mock;
}

const preview = (refund: number) => ({
  allowed: true,
  paid_cents: 9000,
  refund_cents: refund,
  refund_as: 'refund',
  tier: { min_days_before: 7, refund_bp: 10000 },
});

describe('customer area booking', () => {
  beforeEach(() => window.sessionStorage.clear());

  it('shows the booking with QR, documents, receipts and residual', async () => {
    render([{ path: `${P}/account/bookings/${ID}`, body: booking }]);
    const detail = await screen.findByTestId('account-booking');
    expect(within(detail).getByText('K7M2Q9XA')).toBeInTheDocument();
    expect(screen.getByTestId('account-qr')).toHaveTextContent(`PT1:${ID}`);
    expect(screen.getByTestId('account-documents')).toHaveTextContent('Verificato');
    expect(screen.getByTestId('account-missing-documents')).toHaveTextContent('Patente nautica');
    expect(screen.getByTestId('account-receipts')).toHaveTextContent('07/11/2026');
    expect(within(detail).getByText('A-12')).toBeInTheDocument();
  });

  it('cancels with the previewed refund and asks again when the refund changed', async () => {
    let cancelCalls = 0;
    let previews = 0;
    const mock = render([
      { path: `${P}/account/bookings/${ID}`, body: booking },
      {
        path: (u) => u.pathname === `${P}/account/bookings/${ID}/cancellation` && previews++ >= 0,
        body: preview(9000),
      },
      {
        method: 'POST',
        path: (u) => u.pathname === `${P}/account/bookings/${ID}/cancel` && cancelCalls++ === 0,
        status: 409,
        body: problem('refund_changed', 409),
      },
      {
        method: 'POST',
        path: `${P}/account/bookings/${ID}/cancel`,
        body: { status: 'cancelled', refund_cents: 9000, refund_as: 'refund' },
      },
      { path: `${P}/account/bookings`, body: { items: [] } },
    ]);
    fireEvent.click(await screen.findByTestId('account-cancel'));
    const dialog = await screen.findByTestId('account-cancel-dialog');
    expect(await within(dialog).findByTestId('account-refund')).toHaveTextContent('Rimborso');
    fireEvent.click(within(dialog).getByTestId('account-cancel-confirm'));
    expect(await within(dialog).findByTestId('account-refund-changed')).toBeInTheDocument();
    await waitFor(() => expect(previews).toBeGreaterThanOrEqual(2));
    await waitFor(() => expect(within(dialog).getByTestId('account-cancel-confirm')).toBeEnabled());
    fireEvent.click(within(dialog).getByTestId('account-cancel-confirm'));
    await waitFor(() =>
      expect(screen.queryByTestId('account-cancel-dialog')).not.toBeInTheDocument(),
    );
    const posts = mock.calls.filter(
      (c) => c.method === 'POST' && c.url.pathname.endsWith('/cancel'),
    );
    expect(posts).toHaveLength(2);
    expect(posts[0]?.body).toEqual({ expected_refund_cents: 9000 });
    expect(posts[0]?.headers.get('Porto-Customer-Session')).toBe(SESSION);
    // A new attempt after refund_changed uses a new Idempotency-Key.
    expect(posts[0]?.headers.get('Idempotency-Key')).not.toBe(
      posts[1]?.headers.get('Idempotency-Key'),
    );
  });

  it('refuses unsupported files before upload', () => {
    expect(fileProblem(new File(['x'], 'a.txt', { type: 'text/plain' }))).toBe(
      'account:documents.badType',
    );
    const big = new File([new Uint8Array(10 * 1024 * 1024 + 1)], 'a.pdf', {
      type: 'application/pdf',
    });
    expect(fileProblem(big)).toBe('account:documents.tooLarge');
    expect(fileProblem(new File(['%PDF'], 'a.pdf', { type: 'application/pdf' }))).toBeNull();
  });

  it('goes back to the list when the booking is not the customer’s', async () => {
    render([
      { path: `${P}/account/bookings/${ID}`, status: 404, body: problem('link_invalid', 404) },
      { path: `${P}/account/bookings`, body: { items: [] } },
    ]);
    expect(await screen.findByText('Non ci sono prenotazioni.')).toBeInTheDocument();
  });
});
