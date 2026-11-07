import { expect, test } from '@playwright/test';

import { createItemViaUi, selectOption, shellPath } from '../support/pricing';
import { expectNoSeriousViolations, uniq } from '../support/registry';
import { login } from '../support/session';

// M2 items and classifications (m2-plan §8.4): CRUD of items and berth categories through the UI.

test('creates, edits, deactivates and deletes an item', async ({ page }) => {
  const code = `SRV-${uniq()}`;
  await login(page);
  await createItemViaUi(page, {
    code,
    name: 'Allaccio acqua e2e',
    type: 'service',
    unit: 'piece',
    vatPercent: '22',
    includesVat: false,
  });
  await page.getByLabel('Cerca').fill(code);
  await expect(page.getByRole('button', { name: code, exact: true })).toBeVisible();
  await expect(page.getByText('IVA esclusa')).toBeVisible();

  // A 0 % rate needs the VAT nature.
  await page.getByRole('button', { name: code, exact: true }).click();
  const sheet = page.getByRole('dialog');
  await selectOption(page, sheet, 'Aliquota IVA', '0%');
  await sheet.getByRole('button', { name: 'Salva' }).click();
  await expect(sheet.getByText('Con aliquota 0% indica la natura IVA.')).toBeVisible();
  await selectOption(page, sheet, 'Natura IVA', /^N4 /);
  await sheet.getByRole('button', { name: 'Salva' }).click();
  await expect(page.getByText('Articolo aggiornato.')).toBeVisible();
  await expect(page.getByRole('cell', { name: /^N4/ })).toBeVisible();

  await page.getByRole('button', { name: `Azioni per ${code}` }).click();
  await page.getByRole('menuitem', { name: 'Disattiva' }).click();
  await expect(page.getByText('Articolo disattivato.')).toBeVisible();
  await expect(page.getByText('Non attivo')).toBeVisible();

  // Unused by any price list: it can be deleted.
  await page.getByRole('button', { name: `Azioni per ${code}` }).click();
  await page.getByRole('menuitem', { name: 'Elimina' }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: 'Elimina' }).click();
  await expect(page.getByText('Articolo eliminato.')).toBeVisible();
  await expect(page.getByRole('button', { name: code, exact: true })).toBeHidden();
});

test('creates, renames and deletes a berth category', async ({ page }) => {
  const code = `E2E${uniq()}`;
  await login(page);
  await page.goto(`${shellPath(page)}/pricing/classifications`);
  await page.getByRole('button', { name: 'Nuova categoria' }).click();
  let dialog = page.getByRole('dialog');
  await dialog.getByLabel('Codice', { exact: true }).fill(code);
  await dialog.getByLabel('Nome (italiano)').fill('Categoria e2e');
  await dialog.getByRole('button', { name: 'Salva' }).click();
  await expect(page.getByRole('cell', { name: code, exact: true })).toBeVisible();

  await page.getByRole('button', { name: `Modifica ${code}` }).click();
  dialog = page.getByRole('dialog');
  await expect(dialog.getByLabel('Codice', { exact: true })).toBeDisabled();
  await dialog.getByLabel('Nome (italiano)').fill('Categoria e2e rinominata');
  await dialog.getByRole('button', { name: 'Salva' }).click();
  await expect(page.getByText('Categoria e2e rinominata')).toBeVisible();

  await page.getByRole('button', { name: `Elimina ${code}` }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: 'Elimina' }).click();
  await expect(page.getByRole('cell', { name: code, exact: true })).toBeHidden();

  // Revenue classes: the spec defaults are created on first use.
  await page.getByRole('tab', { name: 'Classi di ricavo' }).click();
  await expect(page.getByRole('row')).not.toHaveCount(1);
});

for (const scheme of ['light', 'dark'] as const) {
  test.describe(`${scheme} theme`, () => {
    test.use({ colorScheme: scheme });

    test('items page has no serious axe findings', async ({ page }) => {
      await login(page);
      await page.goto(`${shellPath(page)}/items`);
      await expect(page.getByRole('heading', { name: 'Articoli e servizi' })).toBeVisible();
      await expectNoSeriousViolations(page, `items/${scheme}`);
      await page.getByRole('button', { name: 'Nuovo articolo' }).click();
      await expect(page.getByRole('dialog')).toBeVisible();
      await expectNoSeriousViolations(page, `item sheet/${scheme}`);
    });
  });
}
