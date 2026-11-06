import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { problem } from '@porto/shared/testing';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../../test/renderApp';
import {
  API,
  CUSTOMER_ID,
  DUPLICATE_ID,
  customer,
  meWithRegistry,
  overview,
  summary,
} from '../test/fixtures';

// The map feature is another builder's; these tests render only the registry routes.
vi.mock('../../maps/routes', () => ({ mapRoutes: () => [] }));

const page = (items = [summary()]) => ({ items, next_cursor: null });

describe('CustomersPage', () => {
  it('lists customers and searches on the server', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/customers`, [
      meRoute(meWithRegistry()),
      { path: `${API}/customers`, body: page() },
    ]);
    expect(await screen.findByRole('link', { name: 'Mario Rossi' })).toBeInTheDocument();
    expect(screen.getByText('RSSMRA85T10A562S')).toBeInTheDocument();
    expect(screen.getByText('Residente', { exact: false })).toBeInTheDocument();

    await userEvent.type(screen.getByLabelText('Cerca'), 'rossi');
    await waitFor(() =>
      expect(calls.some((c) => c.url.searchParams.get('q') === 'rossi')).toBe(true),
    );
  });

  it('shows anonymised customers with the translated label', async () => {
    renderApp(`/m/${MARINA_ID}/customers`, [
      meRoute(meWithRegistry()),
      {
        path: `${API}/customers`,
        body: page([summary({ display_name: '', anonymized_at: '2026-02-01T00:00:00Z' })]),
      },
    ]);
    expect(await screen.findByRole('link', { name: 'Cliente anonimizzato' })).toBeInTheDocument();
  });

  it('is forbidden without customers.read', async () => {
    renderApp(`/m/${MARINA_ID}/customers`, [meRoute(makeMe({}, ['marina.read', 'map.view']))]);
    expect(await screen.findByText('Non hai accesso a questa sezione.')).toBeInTheDocument();
  });
});

describe('NewCustomerPage', () => {
  it('validates the fiscal code, creates with an Idempotency-Key and warns about duplicates', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/customers/new`, [
      meRoute(meWithRegistry()),
      {
        method: 'POST',
        path: `${API}/customers`,
        status: 201,
        body: {
          customer,
          possible_duplicates: [summary({ id: DUPLICATE_ID, display_name: 'M. Rossi' })],
        },
      },
    ]);
    await userEvent.type(await screen.findByLabelText('Nome'), 'Mario');
    await userEvent.type(screen.getByLabelText('Cognome'), 'Rossi');
    await userEvent.type(screen.getByLabelText('Codice fiscale'), 'RSSMRA85T10A562T');
    await userEvent.click(screen.getByRole('button', { name: 'Crea' }));
    expect(await screen.findByText('Codice fiscale non valido.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);

    await userEvent.clear(screen.getByLabelText('Codice fiscale'));
    await userEvent.type(screen.getByLabelText('Codice fiscale'), 'rssmra85t10a562s');
    await userEvent.click(screen.getByRole('button', { name: 'Crea' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    const post = calls.find((c) => c.method === 'POST')!;
    expect(post.headers.get('Idempotency-Key')).toBeTruthy();
    expect(post.body).toMatchObject({
      kind: 'person',
      first_name: 'Mario',
      last_name: 'Rossi',
      fiscal_code: 'RSSMRA85T10A562S',
    });
    expect(await screen.findByText('Possibili duplicati')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'M. Rossi' })).toBeInTheDocument();
  });

  it('puts server field errors on the fields', async () => {
    renderApp(`/m/${MARINA_ID}/customers/new`, [
      meRoute(meWithRegistry()),
      {
        method: 'POST',
        path: `${API}/customers`,
        status: 400,
        body: problem('validation_failed', 400, {
          errors: [{ field: 'vat_number', code: 'vat_invalid' }],
        }),
      },
    ]);
    await userEvent.type(await screen.findByLabelText('Nome'), 'Mario');
    await userEvent.type(screen.getByLabelText('Cognome'), 'Rossi');
    await userEvent.click(screen.getByRole('button', { name: 'Crea' }));
    expect(await screen.findByText('Partita IVA non valida.')).toBeInTheDocument();
  });
});

describe('CustomerPage', () => {
  const routes = (data = overview()) => [
    meRoute(meWithRegistry()),
    { path: `${API}/customers/${CUSTOMER_ID}/overview`, body: data },
    { path: `${API}/customers/${CUSTOMER_ID}/consents`, body: { items: [] } },
  ];

  it('saves the data tab with If-Match and the IBAN only when typed', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/customers/${CUSTOMER_ID}`, [
      ...routes(),
      { method: 'PATCH', path: `${API}/customers/${CUSTOMER_ID}`, body: customer },
    ]);
    expect(await screen.findByRole('heading', { name: 'Mario Rossi' })).toBeInTheDocument();
    expect(screen.getByText(/IT•• •••• 3456/)).toBeInTheDocument();
    const notes = screen.getByLabelText('Note');
    await userEvent.type(notes, 'Cliente storico');
    await userEvent.click(screen.getByRole('button', { name: 'Salva' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    const patch = calls.find((c) => c.method === 'PATCH')!;
    expect(patch.headers.get('If-Match')).toBe('"3"');
    expect(patch.body).toMatchObject({ notes: 'Cliente storico', fiscal_code: 'RSSMRA85T10A562S' });
    expect(patch.body).not.toHaveProperty('iban');
  });

  it('anonymises only after typing the confirmation word', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/customers/${CUSTOMER_ID}`, [
      ...routes(),
      { method: 'POST', path: `${API}/customers/${CUSTOMER_ID}/anonymize`, status: 204 },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Anonimizza' }));
    const dialog = await screen.findByRole('dialog');
    const confirm = within(dialog).getByRole('button', { name: 'Anonimizza definitivamente' });
    expect(confirm).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText(/Per confermare scrivi/), 'ANONIMIZZA');
    await userEvent.click(confirm);
    await waitFor(() =>
      expect(calls.find((c) => c.url.pathname.endsWith('/anonymize'))?.body).toEqual({
        confirm: true,
      }),
    );
  });

  it('merges a suggested duplicate with If-Match on the target', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/customers/${CUSTOMER_ID}`, [
      ...routes(
        overview({ duplicates: [summary({ id: DUPLICATE_ID, display_name: 'M. Rossi' })] }),
      ),
      { path: `${API}/customers`, body: page() },
      { method: 'POST', path: `${API}/customers/${CUSTOMER_ID}/merge`, body: customer },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Unisci duplicati' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('checkbox', { name: /M\. Rossi/ }));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Continua' }));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Unisci' }));
    await waitFor(() => expect(calls.some((c) => c.url.pathname.endsWith('/merge'))).toBe(true));
    const merge = calls.find((c) => c.url.pathname.endsWith('/merge'))!;
    expect(merge.body).toEqual({ source_ids: [DUPLICATE_ID] });
    expect(merge.headers.get('If-Match')).toBe('"3"');
  });

  it('creates a customer document and records a consent', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/customers/${CUSTOMER_ID}`, [
      ...routes(),
      { method: 'POST', path: `${API}/documents`, status: 201, body: {} },
      {
        method: 'POST',
        path: `${API}/customers/${CUSTOMER_ID}/consents`,
        status: 201,
        body: {},
      },
    ]);
    await userEvent.click(await screen.findByRole('tab', { name: /Documenti/ }));
    await userEvent.click(await screen.findByRole('button', { name: 'Aggiungi documento' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Numero'), 'CA1234567');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Salva' }));
    await waitFor(() =>
      expect(calls.some((c) => c.method === 'POST' && c.url.pathname.endsWith('/documents'))).toBe(
        true,
      ),
    );
    const doc = calls.find((c) => c.method === 'POST' && c.url.pathname.endsWith('/documents'))!;
    expect(doc.body).toEqual({
      owner_type: 'customer',
      customer_id: CUSTOMER_ID,
      type: 'identity_card',
      number: 'CA1234567',
    });
    expect(doc.headers.get('Idempotency-Key')).toBeTruthy();

    await userEvent.click(screen.getByRole('tab', { name: 'Consensi' }));
    await userEvent.type(await screen.findByLabelText("Versione dell'informativa"), '2026-01');
    await userEvent.click(screen.getByRole('button', { name: 'Registra un consenso' }));
    await waitFor(() =>
      expect(calls.some((c) => c.method === 'POST' && c.url.pathname.endsWith('/consents'))).toBe(
        true,
      ),
    );
    expect(
      calls.find((c) => c.method === 'POST' && c.url.pathname.endsWith('/consents'))!.body,
    ).toEqual({ kind: 'privacy', policy_version: '2026-01', granted: true, source: 'desk' });
  });

  it('locks an anonymised customer', async () => {
    renderApp(`/m/${MARINA_ID}/customers/${CUSTOMER_ID}`, [
      ...routes(
        overview({
          customer: { ...customer, display_name: '', anonymized_at: '2026-02-01T00:00:00Z' },
        }),
      ),
    ]);
    expect(
      await screen.findByRole('heading', { name: 'Cliente anonimizzato' }),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Anonimizza' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Esporta (GDPR)' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Salva' })).toBeDisabled();
  });
});
