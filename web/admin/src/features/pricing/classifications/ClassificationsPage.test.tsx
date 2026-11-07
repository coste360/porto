import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { MARINA_ID, renderApp } from '../../../test/renderApp';
import { base, categories, pricingRoutes, revenueClasses } from '../test/fixtures';

const path = `/m/${MARINA_ID}/pricing/classifications`;

describe('ClassificationsPage', () => {
  it('lists categories with usage and syncs the unknown codes', async () => {
    const { calls } = renderApp(path, [
      {
        method: 'POST',
        path: `${base}/berth-categories/sync`,
        body: { created: [categories.items[0]] },
      },
      ...pricingRoutes(),
    ]);
    expect(await screen.findByText('Fino a 10 m')).toBeInTheDocument();
    expect(
      screen.getByText('Posti 12 · aree 0 · risorse a terra 0 · regole 1'),
    ).toBeInTheDocument();
    expect(screen.getByText('L15 (3)')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /Sincronizza/ }));
    expect(await screen.findByText('Categorie create: 1')).toBeInTheDocument();
    expect(calls.find((c) => c.method === 'POST')?.headers.get('Idempotency-Key')).toBeTruthy();
  });

  it('creates a category; the code is fixed when editing', async () => {
    const { calls } = renderApp(path, [
      { method: 'POST', path: `${base}/berth-categories`, status: 201, body: categories.items[0] },
      { method: 'PATCH', path: `${base}/berth-categories/c2`, body: categories.items[1] },
      ...pricingRoutes(),
    ]);
    await userEvent.click(await screen.findByRole('button', { name: /Nuova categoria/ }));
    let dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Codice'), 'L15');
    await userEvent.type(within(dialog).getByLabelText('Nome (italiano)'), 'Fino a 15 m');
    await userEvent.click(within(dialog).getByLabelText('Aree dinamiche'));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Salva' }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === 'POST')?.body).toEqual({
        code: 'L15',
        name: { it: 'Fino a 15 m' },
        applies_to: ['berth', 'dynamic_area'],
        sort_order: 0,
      }),
    );
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await userEvent.click(screen.getByRole('button', { name: 'Modifica L12' }));
    dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByLabelText('Codice')).toBeDisabled();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Salva' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    const patch = calls.find((c) => c.method === 'PATCH')!;
    expect(patch.headers.get('If-Match')).toBe('"2"');
    expect(patch.body).not.toHaveProperty('code');
  });

  it('shows revenue classes as a two-level tree and adds a subclass', async () => {
    const { calls } = renderApp(path, [
      { method: 'POST', path: `${base}/revenue-classes`, status: 201, body: revenueClasses[1] },
      ...pricingRoutes(),
    ]);
    await userEvent.click(await screen.findByRole('tab', { name: 'Classi di ricavo' }));
    const rows = screen.getAllByRole('row').slice(1);
    expect(rows.map((r) => within(r).getAllByRole('cell')[1]!.textContent)).toEqual([
      'ormeggi',
      'transiti',
      'servizi',
    ]);
    await userEvent.click(
      screen.getByRole('button', { name: 'Aggiungi una sottoclasse a servizi' }),
    );
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Codice'), 'servizi_acqua');
    await userEvent.type(within(dialog).getByLabelText('Nome (italiano)'), 'Acqua');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Salva' }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === 'POST')?.body).toEqual({
        code: 'servizi_acqua',
        name: { it: 'Acqua' },
        parent_id: 'rc3',
        sort_order: 0,
      }),
    );
  });

  it('shows the customer categories with their counts', async () => {
    renderApp(path, pricingRoutes());
    await userEvent.click(await screen.findByRole('tab', { name: 'Categorie clienti' }));
    const rows = (await screen.findAllByRole('row')).slice(1);
    expect(rows.map((r) => within(r).getAllByRole('cell')[2]!.textContent)).toEqual([
      '12',
      '3',
      '0',
      '1',
      '0',
      '40',
    ]);
    expect(screen.getByText('Senza categoria')).toBeInTheDocument();
  });

  it('marks multihulls among the hull types', async () => {
    renderApp(path, pricingRoutes());
    await userEvent.click(await screen.findByRole('tab', { name: 'Tipi di scafo' }));
    expect(screen.getAllByText('Multiscafo')).toHaveLength(2);
  });
});
