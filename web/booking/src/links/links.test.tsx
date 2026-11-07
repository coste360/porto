import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiProvider, createApiClient, type PublicLink } from '@porto/shared/api';
import {
  createFetchMock,
  problem,
  renderWithProviders,
  type MockRoute,
} from '@porto/shared/testing';

import { LINK_PURPOSES, type LinkPurpose } from '../routes';
import { LinkPage } from './index';
import { EMPTY_DATA, acceptBody, linkPath } from './quoteAccept';

const SLUG = 'porto-demo';
const TOKEN = 'ABCDEFGHIJKLMNOP1234';
const L = `/api/public/v1/${SLUG}/links/${TOKEN}`;

const marina = { name: 'Marina Prova', default_locale: 'it' as const, locales: ['it' as const] };

const links: Record<LinkPurpose, PublicLink> = {
  quote: {
    purpose: 'quote',
    marina,
    expires_at: '2026-12-01T00:00:00Z',
    quote: {
      code: 'PRV-2026-0001',
      status: 'sent',
      valid_until: '2026-12-01',
      vessel_name: 'Libeccio',
      options: [
        {
          id: 'o1',
          seq: 1,
          label: 'Pontile A',
          starts_on: '2027-01-01',
          ends_on: '2028-01-01',
          total_cents: 300_000,
          deposit_cents: 90_000,
        },
      ],
      required_fields: ['vessel_registration'],
      missing_fields: ['vessel_registration'],
    },
  },
  renewal: {
    purpose: 'renewal',
    marina,
    expires_at: '2026-12-07T00:00:00Z',
    renewal: {
      status: 'sent',
      deadline_on: '2026-12-07',
      vessel_name: 'Libeccio',
      old_total_cents: 300_000,
      new_total_cents: 309_000,
      new_starts_on: '2027-01-01',
      new_ends_on: '2028-01-01',
    },
  },
  signature: {
    purpose: 'signature',
    marina,
    expires_at: '2026-12-07T00:00:00Z',
    signature: {
      request_status: 'sent',
      signer_status: 'invited',
      role: 'customer',
      document_title: 'Contratto ORM-2027-0001',
      identity: { name: 'Mario Rossi', fiscal_code: 'RSSMRA80A01H501U' },
      otp_channel: 'email',
      attempts_left: 5,
      signature_kind: 'firma_elettronica_semplice',
    },
  },
  unsubscribe: {
    purpose: 'unsubscribe',
    marina,
    expires_at: '2027-01-01T00:00:00Z',
    unsubscribe: { subscribed: true },
  },
};

function renderLink(purpose: LinkPurpose, routes: MockRoute[]) {
  const mock = createFetchMock(routes);
  const api = createApiClient({ fetch: mock.fetch, baseUrl: 'http://localhost' });
  renderWithProviders(
    <ApiProvider client={api}>
      <LinkPage slug={SLUG} purpose={purpose} token={TOKEN} />
    </ApiProvider>,
  );
  return mock;
}

beforeEach(() => {
  URL.createObjectURL = vi.fn(() => 'blob:doc');
  URL.revokeObjectURL = vi.fn();
});

describe('booking link routes by purpose', () => {
  it.each(LINK_PURPOSES)('renders the %s page from the resolved link', async (purpose) => {
    renderLink(purpose, [
      { path: L, body: links[purpose] },
      { path: `${L}/document`, body: {} },
    ]);
    expect(await screen.findByText('Marina Prova')).toBeInTheDocument();
    const expected: Record<LinkPurpose, RegExp> = {
      quote: /Accetto il preventivo/,
      renewal: /Accetto il rinnovo/,
      signature: /Inviami il codice/,
      unsubscribe: /Conferma la disiscrizione/,
    };
    expect(await screen.findByRole('button', { name: expected[purpose] })).toBeInTheDocument();
  });

  it('shows the same invalid page for unknown links and for a link of another purpose', async () => {
    renderLink('quote', [{ path: L, status: 404, body: problem('link_invalid', 404) }]);
    expect(await screen.findByText('Il link non è valido o è scaduto.')).toBeInTheDocument();
  });

  it('refuses a token used under another purpose', async () => {
    renderLink('renewal', [{ path: L, body: links.signature }]);
    expect(await screen.findByText('Il link non è valido o è scaduto.')).toBeInTheDocument();
  });
});

describe('signing page states', () => {
  const routes = (extra: MockRoute[] = []): MockRoute[] => [
    { path: L, body: links.signature },
    { path: `${L}/document`, body: {} },
    ...extra,
  ];

  it('requires the identity confirmation, then shows the OTP input with attempts left', async () => {
    const mock = renderLink(
      'signature',
      routes([
        {
          method: 'POST',
          path: `${L}/signature/otp`,
          body: { signer_status: 'invited', request_status: 'sent', attempts_left: 5 },
        },
        {
          method: 'POST',
          path: `${L}/signature/verify`,
          status: 422,
          body: { ...problem('otp_invalid', 422), details: { attempts_left: 3 } },
        },
      ]),
    );
    const request = await screen.findByRole('button', { name: 'Inviami il codice' });
    expect(request).toBeDisabled();
    expect(screen.getByText(/firma elettronica semplice/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('checkbox', { name: /Confermo che sono io/ }));
    await userEvent.click(request);
    expect(await screen.findByTestId('attempts-left')).toHaveTextContent('5');
    expect(mock.calls.find((c) => c.url.pathname.endsWith('/otp'))!.body).toEqual({
      identity_confirmed: true,
    });
    const sign = screen.getByRole('button', { name: 'Firma' });
    expect(sign).toBeDisabled();
    await userEvent.keyboard('{Tab}');
    await userEvent.type(screen.getByRole('textbox'), '123456');
    await waitFor(() => expect(sign).toBeEnabled());
    await userEvent.click(sign);
    expect(await screen.findByText('Codice errato.')).toBeInTheDocument();
    expect(screen.getByTestId('attempts-left')).toHaveTextContent('3');
  });

  it('shows the locked and rate-limited states', async () => {
    renderLink(
      'signature',
      routes([
        {
          method: 'POST',
          path: `${L}/signature/otp`,
          status: 429,
          body: problem('otp_rate_limited', 429),
          headers: { 'Retry-After': '600' },
        },
      ]),
    );
    await userEvent.click(await screen.findByRole('checkbox', { name: /Confermo che sono io/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Inviami il codice' }));
    expect(await screen.findByText('Hai chiesto troppi codici.')).toBeInTheDocument();
  });

  it('shows the success page with the deposit instructions', async () => {
    renderLink('signature', [
      {
        path: L,
        body: {
          ...links.signature,
          signature: {
            ...links.signature.signature!,
            signer_status: 'signed',
            request_status: 'signed',
            deposit: {
              amount_cents: 90_000,
              due_on: '2026-11-14',
              bank_details: { iban: 'IT60X0542811101000000123456' },
            },
          },
        },
      },
    ]);
    expect(await screen.findByText('Documento firmato. Grazie!')).toBeInTheDocument();
    expect(screen.getByTestId('deposit')).toHaveTextContent('900,00');
    expect(screen.getByTestId('deposit')).toHaveTextContent('IT60X0542811101000000123456');
  });
});

describe('quote acceptance', () => {
  it('asks the missing vessel data and sends only that block', async () => {
    const mock = renderLink('quote', [
      { path: L, body: links.quote },
      { path: `${L}/document`, body: {} },
      {
        method: 'POST',
        path: `${L}/quote/accept`,
        body: {
          contract_code: 'ORM-2027-0001',
          signature_link: `http://porto.test/b/${SLUG}/m/signature/ZZZZZZZZZZZZZZZZ1234`,
        },
      },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Accetto il preventivo' }));
    expect(await screen.findByText('Indica la matricola della barca.')).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText('Matricola'), 'GE1234');
    await userEvent.type(screen.getByLabelText("Scadenza dell'assicurazione"), '2027-06-30');
    await userEvent.click(screen.getByRole('button', { name: 'Accetto il preventivo' }));
    expect(await screen.findByRole('link', { name: 'Firma il contratto' })).toHaveAttribute(
      'href',
      `/b/${SLUG}/m/signature/ZZZZZZZZZZZZZZZZ1234`,
    );
    expect(mock.calls.find((c) => c.url.pathname.endsWith('/quote/accept'))!.body).toEqual({
      option_id: 'o1',
      vessel: { registration: 'GE1234', insurance_expires_on: '2027-06-30' },
    });
  });

  it('builds the customer block only when customer data is missing', () => {
    const q = { missing_fields: ['address', 'fiscal_code'] as const };
    expect(
      acceptBody(
        { missing_fields: [...q.missing_fields] },
        'o1',
        'standard',
        EMPTY_DATA,
        new Set(),
      ),
    ).toEqual({
      ok: false,
      error: 'address',
    });
    const r = acceptBody(
      { missing_fields: ['address', 'fiscal_code'] },
      'o1',
      'early_payment',
      {
        ...EMPTY_DATA,
        address: { line1: 'Via Roma 1', postal_code: '16100', city: 'Genova', country: 'IT' },
        fiscalCode: 'rssmra80a01h501u',
        birthDate: '1980-01-01',
      },
      new Set(),
    );
    expect(r).toEqual({
      ok: true,
      body: {
        option_id: 'o1',
        plan: 'early_payment',
        customer: {
          address: { line1: 'Via Roma 1', postal_code: '16100', city: 'Genova', country: 'IT' },
          fiscal_code: 'RSSMRA80A01H501U',
          birth_date: '1980-01-01',
        },
      },
    });
    expect(
      acceptBody(
        { missing_fields: ['insurance_document'] },
        'o1',
        'standard',
        EMPTY_DATA,
        new Set(),
      ),
    ).toEqual({
      ok: false,
      error: 'uploads',
    });
    expect(linkPath('https://x.test/b/s/m/signature/T?lang=en')).toBe('/b/s/m/signature/T?lang=en');
  });
});
