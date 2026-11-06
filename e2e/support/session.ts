import { expect, type Page } from '@playwright/test';

import { OWNER } from './env';

/** Signs in through the login form and waits until the admin shell is shown. */
export async function login(
  page: Page,
  email: string = OWNER.email,
  password: string = OWNER.password,
) {
  await page.goto('/admin/login');
  await fillLogin(page, email, password);
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
