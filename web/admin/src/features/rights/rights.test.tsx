import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { MARINA_ID, makeMe, meRoute, renderApp } from '../../test/renderApp';

vi.mock('../operations/public', () => ({ opsMapSearch: () => ({}), PlacementPicker: () => null }));
vi.mock('../operations/routes', () => ({ operationsRoutes: () => [] }));
vi.mock('../maps/routes', () => ({ mapRoutes: () => [] }));

const API = `/api/admin/v1/marinas/${MARINA_ID}`;
const me = makeMe({}, ['marina.read', 'rights.manage', 'customers.read', 'map.view']);
const base = [
  meRoute(me),
  {
    path: API,
    body: { id: MARINA_ID, name: 'Marina Prova', slug: 'prova', timezone: 'Europe/Rome' },
  },
];

const table = {
  id: 't1',
  name: 'Generale',
  code: 'GEN',
  status: 'draft',
  entries_count: 0,
  sum_millis: 0,
  version: 3,
};
const berths = {
  items: [
    { id: 'b1', code: 'A01' },
    { id: 'b2', code: 'A02' },
    { id: 'b3', code: 'A03' },
  ],
  next_cursor: null,
};

describe('millesimal grid', () => {
  it('sums live, imports a CSV by berth code and activates only at 1000', async () => {
    const { calls } = renderApp(`/m/${MARINA_ID}/common-charges`, [
      ...base,
      { path: `${API}/millesimal-tables`, body: { items: [table] } },
      { path: `${API}/berths`, body: berths },
      {
        path: `${API}/millesimal-tables/t1/entries`,
        body: { table_id: 't1', entries: [{ berth_id: 'b1', millis: 400 }], sum_millis: 400 },
      },
      {
        method: 'PUT',
        path: `${API}/millesimal-tables/t1/entries`,
        body: { table_id: 't1', entries: [], sum_millis: 1000 },
      },
    ]);
    const sum = await screen.findByTestId('millesimal-sum');
    expect(sum).toHaveTextContent('Somma 400 ‰ (mancano 600)');
    const activate = screen.getByRole('button', { name: 'Attiva' });
    expect(activate).toBeDisabled();
    fireEvent.change(screen.getByLabelText('A02'), { target: { value: '100' } });
    expect(sum).toHaveTextContent('Somma 500 ‰');
    const file = new File(['Posto;Millesimi\nA02;250\nA03;350\nZ99;1\n'], 'm.csv', {
      type: 'text/csv',
    });
    await userEvent.upload(screen.getByLabelText(/Importa CSV/), file);
    await waitFor(() => expect(sum).toHaveTextContent('Somma 1000 ‰ (mancano 0)'));
    expect(screen.getByRole('status')).toHaveTextContent('1 posti sconosciuti');
    expect(activate).toBeEnabled();
    await userEvent.click(screen.getByRole('button', { name: 'Salva' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const put = calls.find((c) => c.method === 'PUT')!;
    expect(put.headers.get('If-Match')).toBe('"3"');
    expect(put.body).toEqual({
      entries: [
        { berth_id: 'b1', millis: 400 },
        { berth_id: 'b2', millis: 250 },
        { berth_id: 'b3', millis: 350 },
      ],
    });
  });
});

describe('right wizard holders', () => {
  it('shows the live Σ‰ of the holders and splits equal shares', async () => {
    renderApp(`/m/${MARINA_ID}/rights`, [
      ...base,
      { path: `${API}/rights`, body: { items: [], next_cursor: null } },
      { path: `${API}/maps`, body: { items: [] } },
      { path: `${API}/customers`, body: { items: [], next_cursor: null } },
      { path: `${API}/vessels`, body: { items: [], next_cursor: null } },
    ]);
    await userEvent.click(await screen.findByRole('button', { name: "Nuovo diritto d'uso" }));
    const dialog = await screen.findByRole('dialog');
    const sum = within(dialog).getByTestId('holders-sum');
    expect(sum).toHaveTextContent('Somma 1000 ‰ (mancano 0)');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Aggiungi titolare' }));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Aggiungi titolare' }));
    expect(sum).toHaveTextContent('Somma 1000 ‰');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Quote uguali' }));
    expect(within(dialog).getByLabelText('Quota (‰)', { selector: '#right-share-0' })).toHaveValue(
      '334',
    );
    expect(within(dialog).getByLabelText('Quota (‰)', { selector: '#right-share-2' })).toHaveValue(
      '333',
    );
    expect(sum).toHaveTextContent('Somma 1000 ‰ (mancano 0)');
    fireEvent.change(within(dialog).getByLabelText('Quota (‰)', { selector: '#right-share-2' }), {
      target: { value: '300' },
    });
    expect(sum).toHaveTextContent('Somma 967 ‰ (mancano 33)');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Crea diritto' }));
    expect(within(dialog).getByText('Le quote devono sommare 1000 ‰.')).toBeInTheDocument();
  });
});

describe('rights list', () => {
  it('shows the berth code, not the berth id, in the berth column', async () => {
    const BERTH_ID = '0193a4c2-1111-7000-8000-000000000001';
    renderApp(`/m/${MARINA_ID}/rights`, [
      ...base,
      {
        path: `${API}/rights`,
        body: {
          items: [
            {
              id: 'r1',
              berth: { resource_type: 'berth', resource_id: BERTH_ID },
              berth_code: 'B12',
              kind: 'long_term_use',
              from_on: '2026-01-01',
              to_on: '2040-12-31',
              title: {},
              status: 'active',
              holders: [],
              vessels: [],
              allocations: [],
              created_at: '2026-01-01T00:00:00Z',
              version: 1,
            },
          ],
          next_cursor: null,
        },
      },
      { path: `${API}/maps`, body: { items: [] } },
      { path: `${API}/customers`, body: { items: [], next_cursor: null } },
      { path: `${API}/vessels`, body: { items: [], next_cursor: null } },
    ]);
    const row = await screen.findByTestId('right-row');
    expect(within(row).getByRole('link', { name: 'B12' })).toBeInTheDocument();
    expect(row).not.toHaveTextContent(BERTH_ID);
  });
});
