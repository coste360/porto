import { expect, test } from '@playwright/test';

import { OWNER } from '../support/env';
import { waitForResetLink } from '../support/mail';
import { fillLogin, expectShell } from '../support/session';

test('forgot password sends a link that sets a new password', async ({ page }) => {
  const newPassword = 'e2e-reset-password-456';

  await page.goto('/admin/login');
  await page.getByRole('link', { name: 'Password dimenticata?' }).click();
  await page.getByLabel('Email').fill(OWNER.email);
  const linkPromise = waitForResetLink(OWNER.email);
  await page.getByRole('button', { name: 'Invia il link' }).click();
  await expect(page.getByText(/riceverai a breve un'email/)).toBeVisible();

  const link = await linkPromise;
  // The token travels in the fragment, never in the query string.
  expect(new URL(link).search).toBe('');
  await page.goto(new URL(link).pathname + new URL(link).hash);
  await page.getByLabel('Nuova password').fill(newPassword);
  await page.getByLabel('Ripeti la password').fill(newPassword);
  await page.getByRole('button', { name: 'Salva la password' }).click();
  await expect(page.getByText('Password aggiornata. Ora puoi accedere.')).toBeVisible();

  // The same link cannot be used twice (leave the page first: a hash-only change does not reload it).
  await page.goto('/admin/login');
  await page.goto(new URL(link).pathname + new URL(link).hash);
  await page.getByLabel('Nuova password').fill('e2e-another-password-789');
  await page.getByLabel('Ripeti la password').fill('e2e-another-password-789');
  await page.getByRole('button', { name: 'Salva la password' }).click();
  await expect(page.getByRole('alert')).toBeVisible();

  await page.goto('/admin/login');
  await fillLogin(page, OWNER.email, newPassword);
  await expectShell(page);

  // Put the original password back for the other specs.
  await page.goto('/admin/account');
  await page.getByLabel('Password attuale').fill(newPassword);
  await page.getByLabel('Nuova password', { exact: true }).fill(OWNER.password);
  await page.getByLabel('Ripeti la nuova password').fill(OWNER.password);
  await page.getByRole('button', { name: 'Cambia password' }).click();
  await expect(page.getByText('Password aggiornata.').first()).toBeVisible();
});
