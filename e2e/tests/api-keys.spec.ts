import { expect, request, test } from '@playwright/test';

import { marinaIdOf } from '../support/api';
import { BASE_URL } from '../support/env';
import { login } from '../support/session';

test('owner creates a scoped API key, uses it once, then revokes it', async ({ page }) => {
  const name = `Gestionale ${Date.now()}`;

  await login(page);
  const marinaId = marinaIdOf(page);
  await page.getByRole('link', { name: 'Chiavi API' }).click();
  await page.getByRole('button', { name: 'Nuova chiave' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Nome').fill(name);
  await dialog.getByLabel('Vedere il porto').check();
  await dialog.getByRole('button', { name: 'Crea' }).click();

  // The secret is shown once.
  const secret = await dialog.getByLabel('Chiave API').inputValue();
  expect(secret).toMatch(/^po_live_/);
  await dialog.getByRole('button', { name: 'Ho copiato il valore' }).click();
  const row = page.getByRole('row', { name: new RegExp(name) });
  await expect(row).toBeVisible();
  await expect(row).not.toContainText(secret);

  // The key authenticates on its own, without a session.
  const client = await request.newContext({ baseURL: BASE_URL });
  const ok = await client.get(`/api/admin/v1/marinas/${marinaId}`, {
    headers: { Authorization: `Bearer ${secret}` },
  });
  expect(ok.status()).toBe(200);

  await row.getByRole('button', { name: 'Revoca' }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: 'Revoca' }).click();
  await expect(page.getByText('Chiave revocata.')).toBeVisible();
  await expect(row.getByText('Revocato')).toBeVisible();

  const revoked = await client.get(`/api/admin/v1/marinas/${marinaId}`, {
    headers: { Authorization: `Bearer ${secret}` },
  });
  expect(revoked.status()).toBe(401);
  await client.dispose();
});
