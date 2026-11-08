import { expect, test } from '@playwright/test';

import { login } from '../support/session';
import { createCustomer, expectNoSeriousViolations, uniq } from '../support/registry';

test('adds a vessel with two owners and a billing company', async ({ page }) => {
  const tag = uniq();
  await login(page);
  const anna = await createCustomer(page, {
    kind: 'person',
    first_name: 'Anna',
    last_name: `Vela${tag}`,
  });
  const bruno = await createCustomer(page, {
    kind: 'person',
    first_name: 'Bruno',
    last_name: `Vela${tag}`,
  });
  const company = await createCustomer(page, {
    kind: 'company',
    company_name: `Charter ${tag} SRL`,
  });

  await page.getByRole('link', { name: 'Imbarcazioni', exact: true }).click();
  await page.getByRole('link', { name: 'Nuova barca' }).click();
  await page.getByLabel('Nome', { exact: true }).fill(`Maestrale ${tag}`);
  await page.getByLabel('Matricola o numero di iscrizione').fill(`GE${tag}`);
  await page.getByLabel('Lunghezza fuori tutto').fill('12,40');
  await page.getByLabel('Baglio').fill('4,05');

  // The server search matches one word of the name; the option shows the display name ("Last First").
  const pick = async (n: number, name: string) => {
    await page.getByRole('combobox', { name: `Cliente ${n}` }).click();
    await page.getByPlaceholder('Cerca un cliente…').fill(name.split(' ')[0]!);
    await page.getByRole('option', { name }).click();
  };
  await page.getByRole('button', { name: 'Aggiungi cliente' }).click();
  await pick(1, anna.display_name);
  await page.getByLabel('Quota').first().fill('60');
  await page.getByRole('button', { name: 'Aggiungi cliente' }).click();
  await pick(2, bruno.display_name);
  // A second row starts as a contact: make it an owner with the remaining share.
  const rows = page.getByTestId('party-row');
  await rows.nth(1).getByRole('combobox').nth(1).click();
  await page.getByRole('option', { name: 'Proprietario' }).click();
  await rows.nth(1).getByLabel('Quota').fill('40');
  await page.getByRole('button', { name: 'Aggiungi cliente' }).click();
  await pick(3, company.display_name);
  await rows.nth(2).getByRole('combobox').nth(1).click();
  await page.getByRole('option', { name: 'Intestatario addebiti' }).click();
  await expect(page.getByText('Quote dei proprietari: 100%')).toBeVisible();

  await page.getByRole('button', { name: 'Crea' }).click();
  await expect(page.getByRole('heading', { name: `Maestrale ${tag}` })).toBeVisible();
  await expect(page.getByText('12,40 m × 4,05 m', { exact: false }).first()).toBeVisible();

  await page.getByRole('tab', { name: /Proprietari e referenti/ }).click();
  await expect(page.getByTestId('party-row')).toHaveCount(3);
  // Reload first so that no toast is on screen (toasts are checked by the M0 a11y spec).
  await page.reload();
  await page.getByRole('tab', { name: /Proprietari e referenti/ }).click();
  await expect(page.getByTestId('party-row')).toHaveCount(3);
  await expectNoSeriousViolations(page, 'vessel page');

  // Shares above 100 % are refused before reaching the server.
  await page.getByTestId('party-row').nth(0).getByLabel('Quota').fill('70');
  await page.getByRole('button', { name: 'Salva proprietari e referenti' }).click();
  await expect(page.getByText('Le quote dei proprietari superano il 100%.')).toBeVisible();
});
