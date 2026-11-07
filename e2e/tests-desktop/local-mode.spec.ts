import { expect, test } from '@playwright/test';

import { signInOwner } from '../support/desktop';

test('local mode explains the online features and the update banner stays off', async ({
  page,
}) => {
  await signInOwner(page);
  const marinaId = new URL(page.url()).pathname.match(/\/admin\/m\/([^/]+)/)![1]!;

  // The version check is off (PORTO_UPDATE_CHECK=false): no banner, and the dashboard explains local mode.
  await expect(page.getByTestId('dashboard-local-mode-card')).toBeVisible();
  await expect(page.getByTestId('update-banner')).toHaveCount(0);

  await page.goto(`/admin/m/${marinaId}/settings/payments`);
  const notice = page.getByTestId('local-mode-notice');
  await expect(notice).toBeVisible();
  await expect(notice).toHaveAttribute('data-feature', 'onlinePayments');
  await notice.getByRole('link', { name: 'Come passare a un server online' }).click();

  await expect(page).toHaveURL(/\/admin\/desktop\/online$/);
  await expect(page.getByTestId('move-online-page')).toBeVisible();
  await expect(page.getByTestId('online-step-server')).toContainText('porto restore');
  // The owner in the app window gets the backup button.
  await expect(page.getByRole('button', { name: 'Fai un backup ora' })).toBeVisible();
});
