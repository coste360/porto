import { expect, test } from '@playwright/test';

import { marinaIdOf } from '../support/api';
import { login } from '../support/session';

// M5 payment providers (m5-plan §5.4, §5.7, §11.5): Porto works offline; the online provider is disabled by
// default, its secrets are write-only (masked, never echoed back), and saving without typing keeps them.

test('payment settings: disabled by default, secrets masked and write-only', async ({ page }) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  await page.goto(`/admin/m/${marinaId}/settings/payments`);
  await expect(page.getByTestId('offline-banner')).toContainText(
    'Porto funziona anche senza pagamenti online',
  );
  await expect(page.getByText('Sempre attivo')).toBeVisible();
  const stripe = page.getByTestId('provider-stripe');
  await expect(page.getByTestId('provider-state')).toHaveText('Non attivo');
  const secret = stripe.getByLabel('Chiave segreta');
  await expect(secret).toHaveAttribute('type', 'password');
  await expect(secret).toHaveValue('');
  await expect(secret).toHaveAttribute('placeholder', 'Non impostata');
  await expect(stripe.getByRole('button', { name: 'Prova connessione' })).toBeDisabled();

  await secret.fill('sk_test_e2e_not_a_real_key');
  await stripe.getByLabel('Segreto del webhook').fill('whsec_e2e_not_real');
  await stripe.getByRole('button', { name: 'Salva' }).click();
  await expect(page.getByText('Impostazioni salvate')).toBeVisible();
  await expect(stripe.getByLabel('Chiave segreta')).toHaveValue('');
  await expect(stripe.getByLabel('Chiave segreta')).toHaveAttribute('placeholder', /Impostata/);
  await expect(page.getByTestId('provider-state')).toHaveText('Non attivo');
  const html = await page.content();
  expect(html).not.toContain('sk_test_e2e_not_a_real_key');
  expect(html).not.toContain('whsec_e2e_not_real');
});
