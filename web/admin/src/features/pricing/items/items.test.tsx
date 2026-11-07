import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { MARINA_ID, renderApp } from '../../../test/renderApp';
import { pickOption } from '../test/dom';
import { base, items, pricingRoutes, reader } from '../test/fixtures';

const path = `/m/${MARINA_ID}/items`;

describe('ItemsPage', () => {
  it('lists items with type, unit and VAT treatment', async () => {
    renderApp(path, pricingRoutes());
    expect(await screen.findByRole('button', { name: 'TRN-NTT' })).toBeInTheDocument();
    expect(screen.getByText('SRV-ALG')).toBeInTheDocument();
    expect(screen.getAllByText('IVA inclusa')).toHaveLength(1);
    expect(screen.getAllByText('IVA esclusa')).toHaveLength(1);
    expect(screen.getByText('Ormeggi / Transiti')).toBeInTheDocument();
  });

  it('creates an item: a 0 % rate needs a nature, the code is upper-cased', async () => {
    const { calls } = renderApp(path, [
      { method: 'POST', path: `${base}/items`, status: 201, body: items[1] },
      ...pricingRoutes(),
    ]);
    await userEvent.click(await screen.findByRole('button', { name: /Nuovo articolo/ }));
    const sheet = await screen.findByRole('dialog');
    await userEvent.type(within(sheet).getByLabelText('Codice'), 'srv-elc');
    await userEvent.type(within(sheet).getByLabelText('Nome (italiano)'), 'Elettricità');
    // The default rate comes from the settings (22 %): no nature field.
    expect(within(sheet).queryByLabelText('Natura IVA')).not.toBeInTheDocument();
    await pickOption('Aliquota IVA', '0%', sheet);
    await userEvent.click(within(sheet).getByRole('button', { name: 'Salva' }));
    expect(
      await within(sheet).findByText('Con aliquota 0% indica la natura IVA.'),
    ).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
    await pickOption('Natura IVA', /^N4 - Esenti/, sheet);
    await userEvent.click(within(sheet).getByRole('button', { name: 'Salva' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    const post = calls.find((c) => c.method === 'POST')!;
    expect(post.headers.get('Idempotency-Key')).toBeTruthy();
    expect(post.body).toMatchObject({
      code: 'SRV-ELC',
      name: { it: 'Elettricità' },
      vat_rate_bp: 0,
      vat_nature: 'N4',
      price_includes_vat: false,
      revenue_class_id: 'rc1',
      item_type: 'service',
    });
  });

  it('a custom VAT rate is sent in basis points', async () => {
    const { calls } = renderApp(path, [
      { method: 'PATCH', path: `${base}/items/i2`, body: items[1] },
      ...pricingRoutes(),
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'SRV-ALG' }));
    const sheet = await screen.findByRole('dialog');
    await pickOption('Aliquota IVA', 'Altra aliquota', sheet);
    const custom = within(sheet).getByLabelText('Aliquota personalizzata');
    await userEvent.clear(custom);
    await userEvent.type(custom, '8,5');
    await userEvent.click(within(sheet).getByRole('button', { name: 'Salva' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PATCH')).toBe(true));
    const patch = calls.find((c) => c.method === 'PATCH')!;
    expect(patch.headers.get('If-Match')).toBe('"2"');
    expect(patch.body).toEqual({ vat_rate_bp: 850 });
  });

  it('a delete refused with in_use offers to deactivate', async () => {
    const { calls } = renderApp(path, [
      {
        method: 'DELETE',
        path: `${base}/items/i1`,
        status: 409,
        body: { type: 'in_use', title: 'In use', status: 409, details: { references: 2 } },
      },
      { method: 'PATCH', path: `${base}/items/i1`, body: { ...items[0], active: false } },
      ...pricingRoutes(),
    ]);
    await userEvent.click(await screen.findByRole('button', { name: 'Azioni per TRN-NTT' }));
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Elimina' }));
    const confirm = await screen.findByRole('alertdialog');
    await userEvent.click(within(confirm).getByRole('button', { name: 'Elimina' }));
    expect(await screen.findByText(/riferimenti: 2/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Disattiva' }));
    await waitFor(() =>
      expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ active: false }),
    );
    expect(calls.find((c) => c.method === 'DELETE')?.headers.get('If-Match')).toBe('"4"');
  });

  it('readers see the items without write actions', async () => {
    renderApp(path, pricingRoutes(reader));
    expect(await screen.findByRole('button', { name: 'TRN-NTT' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Nuovo articolo/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Azioni per TRN-NTT' })).not.toBeInTheDocument();
  });
});
