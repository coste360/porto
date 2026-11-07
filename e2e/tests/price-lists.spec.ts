import { expect, test, type Page } from '@playwright/test';

import {
  createItemViaUi,
  createTransitPriceListViaUi,
  pickDate,
  shellPath,
} from '../support/pricing';
import { expectNoSeriousViolations, uniq } from '../support/registry';
import { login } from '../support/session';

// M2 price list editor (m2-plan §8.4): validation issues, save conflict between two tabs, activation at a date,
// read-only active list, axe. The year is two years ahead so that the activation does not overlap the lists
// of the other specs (one active list per night and contract type, INV-24).

const YEAR = new Date().getFullYear() + 2;

async function setupList(page: Page, suffix: string) {
  const itemCode = `TRN-${suffix}`;
  await createItemViaUi(page, {
    code: itemCode,
    name: 'Transito notte e2e',
    type: 'berth_fee',
    unit: 'night',
    vatPercent: '22',
    includesVat: true,
  });
  return createTransitPriceListViaUi(page, {
    code: `T-${suffix}`,
    year: YEAR,
    itemCode,
    periods: [
      { code: 'BASSA', from: `${YEAR}-01-01`, to: `${YEAR}-05-31` },
      { code: 'ALTA', from: `${YEAR}-06-01`, to: `${YEAR}-12-31` },
    ],
    perMeterEuro: { BASSA: '4,50', ALTA: '7,50' },
  });
}

test('edits a draft, shows validation issues, resolves a save conflict and activates at a date', async ({
  page,
  context,
}) => {
  const suffix = uniq();
  await login(page);
  const { priceListId } = await setupList(page, suffix);
  const editor = `${shellPath(page)}/price-lists/${priceListId}`;

  // Validation: a third season over the whole year overlaps the other two (dry run of unsaved changes).
  await page.getByRole('tab', { name: /Periodi/ }).click();
  await page.getByRole('button', { name: 'Aggiungi periodo' }).click();
  await page.getByRole('tab', { name: /Validazione/ }).click();
  await expect(page.getByText('Verifica delle modifiche non salvate')).toBeVisible();
  await expect(
    page.getByRole('button', { name: /Due stagioni o due eventi si sovrappongono/ }).first(),
  ).toBeVisible();
  await page
    .getByRole('button', { name: /Due stagioni o due eventi si sovrappongono/ })
    .first()
    .click();
  await expect(page.getByRole('tab', { name: /Periodi/ })).toHaveAttribute('aria-selected', 'true');
  await page.getByRole('button', { name: 'Rimuovi P3' }).click();
  await expect(page.getByText('Modifiche non salvate.')).toBeHidden();

  // Conflict: this tab has unsaved changes while a second tab saves first.
  await page.getByRole('tab', { name: 'Generale' }).click();
  await page.getByLabel('Nome (inglese)').fill('Transits (this tab)');
  const other = await context.newPage();
  await other.goto(editor);
  await other.getByLabel('Nome (inglese)').fill('Transits (other tab)');
  await other.getByRole('button', { name: 'Salva', exact: true }).click();
  await expect(other.getByText(/^Listino salvato/)).toBeVisible();
  await other.close();

  await page.getByRole('button', { name: 'Salva', exact: true }).click();
  const conflict = page.getByRole('alertdialog');
  await expect(conflict.getByText('Listino modificato da un altro utente')).toBeVisible();
  await expect(conflict.getByTestId('conflict-details')).toBeVisible();
  await conflict.getByRole('button', { name: 'Ricarica' }).click();
  await expect(page.getByLabel('Nome (inglese)')).toHaveValue('Transits (other tab)');

  // Activation from 1 February: the list starts on that date.
  await page.getByRole('button', { name: 'Attiva', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await pickDate(page, dialog.getByLabel('Attivo dal'), `${YEAR}-02-01`);
  await dialog.getByRole('button', { name: 'Attiva', exact: true }).click();
  await expect(page.getByText('Listino attivato.').first()).toBeVisible();
  await expect(page.getByText('Listino pubblicato')).toBeVisible();
  await expect(page.getByText(`Validità: dal 01/02/${YEAR} al 31/12/${YEAR}`)).toBeVisible();

  // Read-only: no save, disabled fields, rules open without "Applica".
  await expect(page.getByLabel('Codice', { exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Salva', exact: true })).toHaveCount(0);
  await page.getByRole('tab', { name: /Regole/ }).click();
  await page.getByRole('button', { name: 'Transito ALTA' }).click();
  await expect(page.getByRole('dialog').getByLabel('Prezzo al metro')).toBeDisabled();
  await expect(page.getByRole('dialog').getByRole('button', { name: 'Applica' })).toHaveCount(0);
});

for (const scheme of ['light', 'dark'] as const) {
  test.describe(`${scheme} theme`, () => {
    test.use({ colorScheme: scheme });

    test('price lists and editor have no serious axe findings', async ({ page }) => {
      await login(page);
      await page.goto(`${shellPath(page)}/price-lists`);
      await expect(page.getByRole('heading', { name: 'Listini', exact: true })).toBeVisible();
      await expectNoSeriousViolations(page, `price lists/${scheme}`);
      const link = page.getByRole('link', { name: /^T-/ }).first();
      if ((await link.count()) > 0) {
        await link.click();
        await expect(page.getByRole('tab', { name: 'Generale' })).toBeVisible();
        await expectNoSeriousViolations(page, `editor general/${scheme}`);
        await page.getByRole('tab', { name: /Periodi/ }).click();
        await expectNoSeriousViolations(page, `editor periods/${scheme}`);
        await page.getByRole('tab', { name: /Regole/ }).click();
        await expectNoSeriousViolations(page, `editor rules/${scheme}`);
      }
    });
  });
}
