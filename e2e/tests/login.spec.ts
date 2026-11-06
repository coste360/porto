import { expect, test } from '@playwright/test';

import { OWNER } from '../support/env';
import { expectShell, fillLogin, login, logout } from '../support/session';

test('owner logs in, sees the marina shell and logs out', async ({ page }) => {
  await login(page);
  await expect(page.getByText(OWNER.marina).first()).toBeVisible();
  await logout(page);

  // The session is gone: a protected page sends back to the login.
  await page.goto('/admin/');
  await expect(page).toHaveURL(/\/admin\/login/);
});

test('wrong password is refused with the same message as an unknown account', async ({ page }) => {
  await page.goto('/admin/login');
  await fillLogin(page, OWNER.email, 'not-the-password-123');
  const wrong = page.getByRole('alert');
  await expect(wrong).toBeVisible();
  const wrongText = (await wrong.textContent()) ?? '';

  await fillLogin(page, 'nobody@porto.test', 'not-the-password-123');
  await expect(page.getByRole('alert')).toHaveText(wrongText);
  await expect(page).toHaveURL(/\/admin\/login/);
});

test('a deep link survives the login redirect', async ({ page }) => {
  await login(page);
  const shellUrl = new URL(page.url());
  await logout(page);

  await page.goto(`${shellUrl.pathname.replace(/\/$/, '')}/team`);
  await expect(page).toHaveURL(/\/admin\/login\?redirect=/);
  await fillLogin(page, OWNER.email, OWNER.password);
  await expectShell(page);
  await expect(page).toHaveURL(/\/team$/);
});
