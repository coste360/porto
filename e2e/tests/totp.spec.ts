import { expect, test, type Page } from '@playwright/test';

import { OWNER } from '../support/env';
import { expectShell, fillLogin, login, logout } from '../support/session';
import { totp } from '../support/totp';

/** Each code is used in its own 30 s step, so a server that refuses replayed codes accepts every one. */
async function codeInNewStep(secret: string, lastStep: { value: number }): Promise<string> {
  for (;;) {
    const now = Date.now();
    const step = Math.floor(now / 30_000);
    const left = 30_000 - (now % 30_000);
    if (step > lastStep.value && left > 3000) {
      lastStep.value = step;
      return totp(secret, now);
    }
    await new Promise((r) => setTimeout(r, Math.min(left + 200, 1000)));
  }
}

async function typeOtp(page: Page, code: string) {
  await page.getByLabel('Codice di verifica').first().click();
  await page.keyboard.type(code);
}

test('owner enrols TOTP, signs in with a code, then disables it', async ({ page }) => {
  test.setTimeout(150_000);
  const lastStep = { value: 0 };

  await login(page);
  await page.goto('/admin/account');
  await page.getByRole('link', { name: 'Attiva la verifica in due passaggi' }).click();
  await page.getByRole('button', { name: "Inizia l'attivazione" }).click();
  const secret = (await page.locator('code').first().innerText()).trim();
  expect(secret).toMatch(/^[A-Z2-7]+=*$/);

  await typeOtp(page, await codeInNewStep(secret, lastStep));
  await page.getByRole('button', { name: 'Attiva' }).click();
  await expect(page.getByText('Codici di recupero')).toBeVisible();
  await page.getByRole('button', { name: 'Ho salvato i codici' }).click();

  await page.goto('/admin/account');
  await expect(page.getByText('Attiva', { exact: true }).first()).toBeVisible();
  await logout(page);

  // Password alone is no longer enough.
  await fillLogin(page, OWNER.email, OWNER.password);
  await expect(page.getByText('Verifica in due passaggi')).toBeVisible();
  await typeOtp(page, await codeInNewStep(secret, lastStep));
  await page.getByRole('button', { name: 'Verifica' }).click();
  await expectShell(page);

  // Restore the initial state for the other specs.
  await page.goto('/admin/account');
  await page.getByRole('button', { name: 'Disattiva' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Password attuale').fill(OWNER.password);
  await dialog.getByLabel('Codice di verifica').fill(await codeInNewStep(secret, lastStep));
  await dialog.getByRole('button', { name: 'Disattiva' }).click();
  await expect(page.getByText('Verifica in due passaggi disattivata.')).toBeVisible();
});
