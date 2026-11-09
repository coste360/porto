import { expect, test } from '@playwright/test';

import { berthCentre, mapToScreen } from '../support/map';
import { archiveOpsHarbour, setupOpsHarbour } from '../support/occupancy';
import { expectNoSeriousViolations, uniq } from '../support/registry';
import { login } from '../support/session';

// Sensori admin (m12-plan §16.1, §17.4): a device is created on a berth by clicking it on the map of the device
// dialog, its key is shown once (never again in the table or the API), the key is rotated with a grace period and
// the new key is shown once; the page has no serious accessibility finding.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('sensors admin: device by map click, key shown once, rotation', async ({ page }) => {
  test.setTimeout(2 * 60_000);
  await login(page);
  const { marinaId, berths } = await setupOpsHarbour(page, { berths: 4 });
  harbour = marinaId;
  const tag = uniq();
  await page.goto(`/admin/m/${marinaId}/sensors`);
  await page.getByTestId('sensors-tab-devices').click();
  await page.getByTestId('device-new').click();
  const dialog = page.getByTestId('device-dialog');
  await dialog.getByLabel('Codice').fill(`S${tag}`);
  await dialog.getByLabel('Identificativo del fornitore').fill(`EXT-${tag}`);
  const svg = dialog.getByTestId('device-map').locator('svg[data-px-per-cm]');
  await expect(svg).toBeVisible();
  const target = berths[0]!;
  const p = await mapToScreen(svg, berthCentre(target));
  await page.mouse.click(p.x, p.y);
  await expect(dialog.getByTestId('device-picked')).toContainText(target.code);
  await dialog.getByTestId('device-submit').click();

  const reveal = page.getByTestId('key-reveal');
  const keyInput = reveal.locator('input[readonly]');
  await expect(keyInput).toHaveValue(/^po_sns_[a-z2-7]{16}_/);
  const key = await keyInput.inputValue();
  await expect(reveal.getByTestId('key-example')).toBeAttached();
  await expectNoSeriousViolations(page, 'sensors key reveal');
  await reveal.getByRole('button', { name: 'Ho copiato il valore' }).click();
  await expect(reveal).toBeHidden();
  const row = page.getByTestId(`device-row-S${tag}`);
  await expect(row).toContainText(target.code);
  await expect(page.getByTestId(`device-key-S${tag}`)).toHaveText('Attiva');
  expect(await page.content()).not.toContain(key);
  const api = await page.request.get(`/api/admin/v1/marinas/${marinaId}/sensors/devices`);
  expect(await api.text()).not.toContain(key.split('_').at(-1)!);

  // Rotation with a grace period: the new key is shown once, the status is "in rotazione".
  await page.getByTestId(`device-rotate-S${tag}`).click();
  await page.getByTestId('rotate-dialog').getByLabel('Tolleranza (ore, 0-168)').fill('2');
  await page.getByTestId('rotate-submit').click();
  await expect(keyInput).toHaveValue(/^po_sns_/);
  expect(await keyInput.inputValue()).not.toBe(key);
  await reveal.getByRole('button', { name: 'Ho copiato il valore' }).click();
  await expect(page.getByTestId(`device-key-S${tag}`)).toHaveText('In rotazione');
  await expectNoSeriousViolations(page, 'sensors devices');
});
