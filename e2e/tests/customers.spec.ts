import { expect, test } from '@playwright/test';

import { login } from '../support/session';
import {
  createCustomer,
  expectNoSeriousViolations,
  fakeFiscalCode,
  uniq,
} from '../support/registry';

test('creates, edits and searches a customer', async ({ page }) => {
  const last = `Ormeggi${uniq()}`;
  const fiscal = fakeFiscalCode();
  await login(page);
  await page.getByRole('link', { name: 'Clienti' }).click();
  await page.getByRole('link', { name: 'Nuovo cliente' }).click();

  await page.getByLabel('Nome', { exact: true }).fill('Marta');
  await page.getByLabel('Cognome').fill(last);
  // A wrong check character is refused on the client.
  await page
    .getByLabel('Codice fiscale')
    .fill(`${fiscal.slice(0, 15)}${fiscal[15] === 'A' ? 'B' : 'A'}`);
  await page.getByRole('button', { name: 'Crea' }).click();
  await expect(page.getByText('Codice fiscale non valido.')).toBeVisible();
  await page.getByLabel('Codice fiscale').fill(fiscal);
  await page
    .getByRole('textbox', { name: 'Email 1', exact: true })
    .fill(`marta.${last.toLowerCase()}@example.invalid`);
  await page.getByRole('button', { name: 'Crea' }).click();
  await expect(page.getByRole('heading', { name: `${last} Marta` })).toBeVisible();

  await page.getByLabel('Note').fill('Cliente di prova e2e');
  await page.getByRole('button', { name: 'Salva' }).click();
  await expect(page.getByText('Cliente aggiornato.')).toBeVisible();

  await page.getByRole('link', { name: 'Clienti' }).click();
  await page.getByLabel('Cerca').fill(fiscal.toLowerCase());
  await expect(page.getByRole('link', { name: `${last} Marta` })).toBeVisible();

  await page.getByRole('link', { name: `${last} Marta` }).click();
  await expect(page.getByRole('heading', { name: `${last} Marta` })).toBeVisible();
  // Reload first so that no toast is on screen (toasts are checked by the M0 a11y spec).
  await page.reload();
  await expectNoSeriousViolations(page, 'customer page');
});

test('merges a duplicate and anonymises a customer with confirmations', async ({ page }) => {
  const last = `Doppio${uniq()}`;
  const fiscal = fakeFiscalCode();
  await login(page);
  const target = await createCustomer(page, {
    kind: 'person',
    first_name: 'Luca',
    last_name: last,
    fiscal_code: fiscal,
  });
  await createCustomer(page, {
    kind: 'person',
    first_name: 'L.',
    last_name: last,
    fiscal_code: fiscal,
  });

  const shell = new URL(page.url()).pathname.replace(/\/$/, '');
  await page.goto(`${shell}/customers/${target.id}`);
  await expect(page.getByText('Possibili duplicati')).toBeVisible();
  await page.getByRole('button', { name: 'Unisci duplicati' }).click();
  const merge = page.getByRole('dialog');
  await merge.getByRole('checkbox', { name: new RegExp(`${last} L\\.`) }).check();
  await merge.getByRole('button', { name: 'Continua' }).click();
  await merge.getByRole('button', { name: 'Unisci' }).click();
  await expect(page.getByText('Clienti uniti.')).toBeVisible();

  await page.getByRole('button', { name: 'Anonimizza' }).click();
  const anonymize = page.getByRole('dialog');
  const confirm = anonymize.getByRole('button', {
    name: 'Anonimizza definitivamente',
  });
  await expect(confirm).toBeDisabled();
  await anonymize.getByLabel(/Per confermare scrivi/).fill('ANONIMIZZA');
  await confirm.click();
  await expect(page.getByRole('heading', { name: 'Cliente anonimizzato' })).toBeVisible();
  await expect(page.getByText(fiscal)).toHaveCount(0);
});
