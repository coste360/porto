import { expect, test } from '@playwright/test';

import { csrfHeaders, DESKTOP_OWNER, signInOwner } from '../support/desktop';

// M6b kiosk (m6b-plan §7.6): the setting on the desktop app page and the exit page with the credentials of an
// owner (local mode). The window behaviour itself (full screen, hidden Dock and menu bar) is a manual check: the
// suite runs the headless launcher.

test('the kiosk switch is saved for the next launch', async ({ page }) => {
  await signInOwner(page);
  await page.goto('/admin/desktop');
  const card = page.getByTestId('desktop-kiosk-card');
  await expect(card).toBeVisible();
  const put = page.waitForResponse((r) => r.url().endsWith('/api/desktop/v1/kiosk'));
  await card.getByRole('switch').click();
  expect((await put).status()).toBe(200);
  await expect(page.getByText('Impostazione salvata: vale dal prossimo avvio.')).toBeVisible();
  // Off again, so the other specs are not affected at the next launch.
  const res = await page.request.put('/api/desktop/v1/kiosk', {
    data: { enabled: false },
    headers: await csrfHeaders(page),
  });
  expect(res.ok()).toBeTruthy();
});

test('the exit page needs an owner: a wrong password is refused, the right one leaves kiosk mode', async ({
  page,
}) => {
  await signInOwner(page);
  await page.goto('/admin/desktop/kiosk-exit');
  const form = page.getByTestId('kiosk-exit-page');
  await form.getByLabel('Email del titolare').fill(DESKTOP_OWNER.email);
  await form.getByLabel('Password del titolare').fill('not-the-password');
  await form.getByRole('button', { name: 'Esci' }).click();
  await expect(form.getByRole('alert')).toBeVisible();
  await form.getByLabel('Password del titolare').fill(DESKTOP_OWNER.password);
  await form.getByRole('button', { name: 'Esci' }).click();
  await expect(page.getByTestId('kiosk-exit-done')).toBeVisible();
});
