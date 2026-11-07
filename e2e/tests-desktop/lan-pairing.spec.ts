import { devices, expect, test } from '@playwright/test';

import { addDockMember, setLan, signInOwner } from '../support/desktop';

const marinaIdOf = (url: string) => new URL(url).pathname.match(/\/admin\/m\/([^/]+)/)![1]!;

async function pairingDialogFor(
  page: import('@playwright/test').Page,
  marinaId: string,
  user: string,
) {
  await page.goto(`/admin/m/${marinaId}/devices`);
  await page.getByRole('button', { name: 'Abbina un dispositivo' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Persona').click();
  await page.getByRole('option', { name: user }).click();
  await dialog.getByRole('button', { name: 'Genera il codice' }).click();
  return dialog;
}

test.describe.configure({ mode: 'serial' });

test('with the local network off the pairing dialog shows the notice and no QR', async ({
  page,
}) => {
  await signInOwner(page);
  await setLan(page, false);
  const marinaId = marinaIdOf(page.url());
  const member = await addDockMember(page, marinaId);
  const dialog = await pairingDialogFor(page, marinaId, member);
  await expect(dialog.getByTestId('pair-unreachable')).toBeVisible();
  await expect(dialog.getByRole('img')).toHaveCount(0);
});

test('a phone scans the QR and opens the dock panel in under a minute', async ({
  page,
  browser,
}) => {
  await signInOwner(page);
  const marinaId = marinaIdOf(page.url());

  // The owner switches the local network on from the desktop app page.
  await page.getByRole('button', { name: /Menu utente/ }).click();
  await page.getByRole('menuitem', { name: 'App desktop' }).click();
  const lan = page.getByTestId('desktop-lan-card');
  const toggle = lan.getByRole('switch', { name: 'Attiva la rete locale' });
  if ((await toggle.getAttribute('aria-checked')) !== 'true') await toggle.click();
  await expect(page.getByTestId('lan-url')).toContainText('https://');

  const member = await addDockMember(page, marinaId);
  const dialog = await pairingDialogFor(page, marinaId, member);
  const panel = dialog.getByTestId('pairing-panel');
  await expect(panel).toBeVisible();
  const pairUrl = (await panel.getAttribute('data-pair-url'))!;
  expect(pairUrl).toMatch(/^https:\/\/.+\/admin\/pair#c=[A-Z0-9]{4}-[A-Z0-9]{4}/);

  // The phone: Pixel 7, the certificate warning accepted (ignoreHTTPSErrors stands for the tap on "proceed").
  const phone = await browser.newContext({
    ...devices['Pixel 7'],
    locale: 'it-IT',
    ignoreHTTPSErrors: true,
  });
  const p = await phone.newPage();
  const t0 = Date.now();
  await p.goto(pairUrl);
  await p.getByRole('button', { name: 'Collega' }).click();
  await expect(p.getByTestId('dock-page')).toBeVisible({ timeout: 30_000 });
  expect(Date.now() - t0).toBeLessThan(60_000);
  // The code never stays in the address bar.
  expect(new URL(p.url()).hash).toBe('');

  // The owner revokes the browser device: the phone is signed out at its next request.
  await page.keyboard.press('Escape');
  await page.reload();
  const row = page.getByRole('row', { name: new RegExp(member) }).filter({ hasText: 'Browser' });
  await expect(row).toBeVisible();
  await row.getByRole('button', { name: 'Revoca' }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: 'Revoca' }).click();
  await expect(page.getByText('Dispositivo revocato.')).toBeVisible();

  await p.reload();
  await expect(p).toHaveURL(/\/admin\/login/);
  await phone.close();

  await setLan(page, false);
});
