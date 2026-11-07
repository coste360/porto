import { expect, test, type Page } from '@playwright/test';

import { marinaIdOf } from '../support/api';
import { shellPath } from '../support/pricing';
import { adminApi, uniq } from '../support/registry';
import { login } from '../support/session';

// M2 renewal wizard (spec 12.6, m2-plan §8.4): the source list is set up through the API, the wizard runs in
// the UI: target, +5 % adjustment, preview of the matrix diff, creation of the draft. Year three ahead, so that
// nothing overlaps the lists of the other specs.

const YEAR = new Date().getFullYear() + 3;

type Doc = { id: string; version: number; code: string };

async function sourceList(page: Page, suffix: string): Promise<Doc> {
  const m = `/api/admin/v1/marinas/${marinaIdOf(page)}`;
  const classes = await adminApi<{ items: { id: string }[] }>(page, 'GET', `${m}/revenue-classes`);
  const item = await adminApi<{ id: string }>(page, 'POST', `${m}/items`, {
    code: `TRN-R${suffix}`,
    name: { it: 'Transito notte rinnovo' },
    item_type: 'berth_fee',
    unit: 'night',
    vat_rate_bp: 2200,
    price_includes_vat: true,
    revenue_class_id: classes.data.items[0]!.id,
  });
  const created = await adminApi<Doc>(page, 'POST', `${m}/price-lists`, {
    code: `R-${suffix}`,
    name: { it: 'Transiti da rinnovare' },
    valid_from: `${YEAR}-01-01`,
    valid_to: `${YEAR}-12-31`,
    contract_types: ['transit'],
  });
  const saved = await adminApi<{ document: Doc }>(
    page,
    'PUT',
    `${m}/price-lists/${created.data.id}`,
    {
      code: `R-${suffix}`,
      name: { it: 'Transiti da rinnovare' },
      valid_from: `${YEAR}-01-01`,
      valid_to: `${YEAR}-12-31`,
      contract_types: ['transit'],
      settings: {
        dimension_source: 'vessel',
        length_rounding: { step_cm: 10, mode: 'up' },
        primary_items: { transit: item.data.id },
      },
      periods: [
        {
          key: 'p:alta',
          code: 'ALTA',
          name: { it: 'Alta' },
          kind: 'season',
          ranges: [{ from: `${YEAR}-01-01`, to: `${YEAR}-12-31` }],
        },
      ],
      bands: [
        { key: 'b:l10', code: 'L10', max_cm: 1000 },
        { key: 'b:l12', code: 'L12', max_cm: 1200 },
      ],
      rules: [
        {
          key: 'r:alta',
          stage: 'base',
          name: 'Transito alta',
          item_id: item.data.id,
          basis: 'per_meter',
          unit: 'night',
          unit_cents: 750,
          scope: { periods: ['ALTA'] },
        },
      ],
    },
    { 'If-Match': `"${created.data.version}"` },
  );
  return saved.data.document;
}

test('renews a list with +5 %: preview diff, then the new draft opens in the editor', async ({
  page,
}) => {
  const suffix = uniq();
  await login(page);
  const source = await sourceList(page, suffix);
  await page.goto(`${shellPath(page)}/price-lists/${source.id}`);
  await page.getByRole('link', { name: 'Rinnova o copia' }).click();

  // 1. Target: shift one year; the code and dates follow.
  await expect(page.getByRole('heading', { name: `Rinnova o copia R-${suffix}` })).toBeVisible();
  await page.getByLabel('Codice', { exact: true }).fill(`R-${suffix}-N`);
  await expect(page.getByLabel('Valido dal')).toContainText(`01/01/${YEAR + 1}`);
  await page.getByRole('button', { name: 'Continua' }).click();

  // 2. Adjustments: +5 % on the whole list.
  await page.getByRole('button', { name: 'Aggiungi adeguamento' }).click();
  await page.getByLabel('Valore').fill('5');
  await page.getByRole('button', { name: 'Anteprima' }).click();

  // 3. Preview: the matrix diff of the transit contract type.
  await expect(page.getByText(/Regole copiate: 1 · importi modificati: 1/)).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Transito' })).toBeVisible();
  await page.getByRole('button', { name: 'Continua' }).click();

  // 4. Create the draft.
  await expect(
    page.getByText(`Bozza R-${suffix}-N, valida dal 01/01/${YEAR + 1} al 31/12/${YEAR + 1}.`),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Crea bozza' }).click();
  await expect(page.getByText(`Bozza R-${suffix}-N creata.`)).toBeVisible();
  await expect(page.getByLabel('Codice', { exact: true })).toHaveValue(`R-${suffix}-N`);
  await expect(
    page.getByText(`Validità: dal 01/01/${YEAR + 1} al 31/12/${YEAR + 1}`),
  ).toBeVisible();
  await page.getByRole('tab', { name: /Regole/ }).click();
  // 7,50 € + 5 % = 7,88 € (half-even on 787,5 cents).
  await expect(page.getByText(/7,88/)).toBeVisible();
});
