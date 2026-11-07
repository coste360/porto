import { expect, test, type Page } from '@playwright/test';

import { OWNER } from './env';

/**
 * Signs in through the login form and waits until the admin shell is shown. The server limits login attempts
 * per IP prefix (30 a minute) and the whole suite signs in from 127.0.0.1, so a 429 is waited out (Retry-After)
 * and the form submitted again; the limit itself is never relaxed for tests.
 */
export async function login(
  page: Page,
  email: string = OWNER.email,
  password: string = OWNER.password,
) {
  await page.goto('/admin/login');
  for (let attempt = 0; ; attempt++) {
    const response = page.waitForResponse(
      (r) => r.url().endsWith('/api/admin/v1/auth/login') && r.request().method() === 'POST',
    );
    await fillLogin(page, email, password);
    const res = await response;
    if (res.status() !== 429 || attempt >= 2) break;
    const wait = Math.min(Number(res.headers()['retry-after'] ?? '5') || 5, 60) + 1;
    test.info().setTimeout(test.info().timeout + wait * 1000);
    await page.waitForTimeout(wait * 1000);
  }
  await expectShell(page);
}

export async function fillLogin(page: Page, email: string, password: string) {
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Accedi' }).click();
}

/** The marina shell: the single marina of the owner opens directly. */
export async function expectShell(page: Page) {
  await expect(page).toHaveURL(/\/admin\/m\/[^/]+/);
  await expect(page.getByRole('button', { name: /Menu utente/ })).toBeVisible();
}

export async function logout(page: Page) {
  await page.getByRole('button', { name: /Menu utente/ }).click();
  await page.getByRole('menuitem', { name: 'Esci' }).click();
  await expect(page).toHaveURL(/\/admin\/login/);
}
