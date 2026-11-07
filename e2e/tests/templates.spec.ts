import { expect, test } from '@playwright/test';

import { login } from '../support/session';
import { uniq } from '../support/registry';

// Templates editor (m4-plan §9.5): create a contract template from the built-in default, insert a field, see
// the error marker of an unknown field, render the preview, publish.

test('edit, error marker, preview renders, publish', async ({ page }) => {
  test.setTimeout(3 * 60_000);
  await login(page);
  const shell = new URL(page.url()).pathname.replace(/\/$/, '');
  await page.goto(`${shell}/templates`);
  await page.getByRole('combobox', { name: 'Tipo' }).first().click();
  await page.getByRole('option', { name: 'Contratto annuale' }).click();
  await page.getByLabel('Codice').fill(`ANN-${uniq()}`);
  await page.getByLabel('Nome').fill('Contratto annuale E2E');
  await page.getByRole('button', { name: 'Crea' }).click();
  const editor = page.getByRole('textbox', { name: /Testo del modello/ });
  await expect(editor).toBeVisible();
  await expect(editor).not.toHaveValue('');

  // Unknown field → the error names the line.
  await editor.fill('Gentile {{customer.nome_sbagliato}}\n');
  await page.getByRole('button', { name: 'Salva bozza' }).click();
  await expect(page.getByTestId('template-issues')).toContainText('riga 1');
  await expect(editor).toHaveAttribute('aria-invalid', 'true');

  // A field from the catalogue tree, then preview and publish.
  await editor.fill('Gentile ');
  await editor.press('End');
  await page.getByRole('button', { name: 'Apri customer' }).click();
  await page
    .getByRole('navigation', { name: 'Campi' })
    .getByRole('button', { name: 'display_name', exact: true })
    .first()
    .click();
  await expect(editor).toHaveValue(/\{\{customer\.display_name\}\}/);
  await page.getByRole('button', { name: 'Anteprima PDF' }).click();
  await expect(page.getByTestId('template-preview')).toHaveAttribute('src', /^blob:/);
  await page.getByRole('button', { name: 'Salva bozza' }).click();
  await expect(page.getByText(/Bozza salvata/)).toBeVisible();
  await page.getByRole('button', { name: 'Pubblica' }).click();
  await expect(page.getByText(/pubblicata/)).toBeVisible();
});
