import { expect, test } from '@playwright/test';

import { waitForResetLink } from '../support/mail';
import { expectShell, login } from '../support/session';

test('owner invites a dock worker, who sets a password and signs in with the dock role', async ({
  page,
  browser,
}) => {
  const email = `dock-${Date.now()}@porto.test`;
  const password = 'e2e-dock-password-123';

  await login(page);
  await page.getByRole('link', { name: 'Team' }).click();
  await page.getByRole('button', { name: 'Aggiungi persona' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Nome').fill('Dario Banchina');
  await dialog.getByLabel('Email').fill(email);
  await dialog.getByLabel('Ruolo').click();
  await page.getByRole('option', { name: 'Ormeggiatore' }).click();
  const linkPromise = waitForResetLink(email);
  await dialog.getByRole('button', { name: 'Aggiungi' }).click();
  await expect(page.getByText('Persona aggiunta.')).toBeVisible();

  const row = page.getByRole('row', { name: /Dario Banchina/ });
  await expect(row).toBeVisible();
  await expect(row.getByText('Invito in attesa')).toBeVisible();

  // The invitee opens the link in their own browser and chooses a password.
  const link = new URL(await linkPromise);
  const invitee = await browser.newContext({ locale: 'it-IT' });
  const other = await invitee.newPage();
  await other.goto(link.pathname + link.hash);
  await other.getByLabel('Nuova password').fill(password);
  await other.getByLabel('Ripeti la password').fill(password);
  await other.getByRole('button', { name: 'Salva la password' }).click();
  await expect(other.getByText('Password aggiornata. Ora puoi accedere.')).toBeVisible();

  await login(other, email, password);
  await expectShell(other);
  // The dock role does not manage the team: no Team entry in the navigation.
  await expect(other.getByRole('link', { name: 'Team' })).toHaveCount(0);
  await invitee.close();

  // Back on the owner's page the invitation is no longer pending; removing the member ends the access.
  await page.reload();
  await expect(row.getByText('Invito in attesa')).toHaveCount(0);
  await row.getByRole('button', { name: 'Rimuovi' }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: 'Rimuovi' }).click();
  await expect(page.getByText('Persona rimossa.')).toBeVisible();
  await expect(page.getByRole('row', { name: /Dario Banchina/ })).toHaveCount(0);
});
