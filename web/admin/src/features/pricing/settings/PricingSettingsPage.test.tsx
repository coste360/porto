import { fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { MARINA_ID, renderApp } from '../../../test/renderApp';
import { base, pricingRoutes, reader, settings } from '../test/fixtures';

const path = `/m/${MARINA_ID}/pricing/settings`;

describe('PricingSettingsPage', () => {
  it('saves the default VAT with If-Match', async () => {
    const { calls } = renderApp(path, [
      { method: 'PUT', path: `${base}/pricing-settings`, body: { ...settings, version: 4 } },
      ...pricingRoutes(),
    ]);
    const rate = await screen.findByLabelText('Aliquota IVA predefinita');
    expect(rate).toHaveValue('22');
    fireEvent.change(rate, { target: { value: '10' } });
    await userEvent.click(screen.getByLabelText('Prezzi IVA inclusa per i nuovi articoli'));
    await userEvent.click(screen.getByRole('button', { name: 'Salva' }));
    await waitFor(() => expect(calls.some((c) => c.method === 'PUT')).toBe(true));
    const put = calls.find((c) => c.method === 'PUT')!;
    expect(put.headers.get('If-Match')).toBe('"3"');
    expect(put.body).toEqual({ default_vat_rate_bp: 1000, default_price_includes_vat: true });
  });

  it('is forbidden without pricing.configure', async () => {
    renderApp(path, pricingRoutes(reader));
    expect(await screen.findByText(/Non hai accesso/)).toBeInTheDocument();
  });
});
