import { expect, test } from '@playwright/test';

import { OWNER } from '../support/env';

test('the booking placeholder shows the marina and the coming-soon notice', async ({ page }) => {
  await page.goto(`/b/${OWNER.slug}`);
  await expect(page.getByRole('heading', { name: OWNER.marina })).toBeVisible();
  await expect(page.getByText('powered by Porto')).toBeVisible();
});

test('an unknown marina shows the not-found page', async ({ page }) => {
  await page.goto('/b/does-not-exist');
  await expect(page.getByText(OWNER.marina)).toHaveCount(0);
  await expect(page.getByText("Controlla l'indirizzo della pagina.")).toBeVisible();
});

test('?lang=en switches the booking page to English', async ({ page }) => {
  await page.goto(`/b/${OWNER.slug}?lang=en`);
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
});
