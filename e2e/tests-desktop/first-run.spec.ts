import { expect, test } from '@playwright/test';

import { DESKTOP_OWNER, firstRunUrl } from '../support/desktop';

// The window flow of a fresh install (m6a-plan §2.6): the launcher opens the first-run URL with a single-use
// token in the fragment; the owner is created in the window, then the marina by the usual panel flow.
test('first run creates the owner and the marina, then the link is spent', async ({ page }) => {
  const url = firstRunUrl();
  expect(url, 'the launcher writes first-run.url (PORTO_DESKTOP_FIRST_RUN_FILE)').not.toBeNull();
  expect(url).toMatch(/^\/admin\/desktop\/first-run#t=/);

  await page.goto(url!);
  const form = page.getByTestId('first-run-page');
  await expect(form).toBeVisible();
  // The token leaves the address bar at once.
  await expect(page).toHaveURL(/\/admin\/desktop\/first-run$/);

  await form.getByLabel("Nome dell'azienda").fill(DESKTOP_OWNER.org);
  await form.getByLabel('Il tuo nome').fill(DESKTOP_OWNER.name);
  await form.getByLabel('Email').fill(DESKTOP_OWNER.email);
  await form.getByLabel('Password', { exact: true }).fill(DESKTOP_OWNER.password);
  await form.getByLabel('Ripeti la password').fill(DESKTOP_OWNER.password);
  await form.getByRole('button', { name: "Crea l'account" }).click();

  // No marina yet: the picker offers to create the first one.
  await expect(page.getByText('Crea il primo porto')).toBeVisible();
  await page.getByLabel('Nome').fill(DESKTOP_OWNER.marina);
  await page.getByLabel('Identificativo').fill(DESKTOP_OWNER.slug);
  await page.getByRole('button', { name: 'Crea porto' }).click();
  await expect(page).toHaveURL(/\/admin\/m\/[^/]+/);
  await expect(page.getByRole('button', { name: /Menu utente/ })).toBeVisible();

  // The URL file is deleted once used, and the same link does not work twice.
  expect(firstRunUrl()).toBeNull();
  const again = await page.context().browser()!.newContext({ locale: 'it-IT' });
  const other = await again.newPage();
  await other.goto(url!);
  await expect(other.getByText('Il primo avvio è già stato completato')).toBeVisible();
  await expect(other.getByRole('button', { name: "Crea l'account" })).toHaveCount(0);
  const res = await other.request.post('/api/desktop/v1/first-run', {
    data: {
      token: new URLSearchParams(url!.split('#')[1]).get('t'),
      organization: 'X',
      name: 'X',
      email: 'x@desktop.test',
      password: 'another-password-123',
      update_check: false,
    },
  });
  expect([400, 409]).toContain(res.status());
  await again.close();
});
