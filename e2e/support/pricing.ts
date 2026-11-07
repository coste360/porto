import { expect, type Locator, type Page } from '@playwright/test';

import { marinaIdOf } from './api';

// Pricing helpers of the M2 specs (m2-plan §8.4): fixed signatures, driven through the admin UI (items page,
// price list dialog and editor). WEB-SIMULATOR uses them only through these signatures.

export type ItemViaUi = {
  code: string;
  name: string;
  type: 'berth_fee' | 'service';
  unit: string;
  vatPercent: string;
  includesVat: boolean;
};

export type TransitPriceListViaUi = {
  code: string;
  year: number;
  itemCode: string;
  periods: { code: string; from: string; to: string }[];
  perMeterEuro: Record<string, string>;
  /**
   * Optional length bands (code, maximum length in metres as typed, e.g. "12", berth category), for the matrix
   * rows. Without bands the list prices per metre only.
   */
  bands?: { code: string; maxMetres: string; category?: string }[];
};

const ITEM_TYPE_LABELS: Record<ItemViaUi['type'], string> = {
  berth_fee: 'Canone di ormeggio',
  service: 'Servizio',
};

const UNIT_LABELS: Record<string, string> = {
  night: 'Notte',
  month: 'Mese',
  season: 'Stagione',
  year: 'Anno',
  m: 'Metro',
  m2: 'Metro quadro',
  kwh: 'kWh',
  m3: 'Metro cubo',
  hour: 'Ora',
  piece: 'Pezzo',
  lump: 'A corpo',
};

const PRESET_VAT = new Set(['22', '10', '5', '4', '0']);

/** The admin shell path of the current marina (/admin/m/<id>). */
export function shellPath(page: Page): string {
  return `/admin/m/${marinaIdOf(page)}`;
}

/** Opens a Radix Select by its accessible name (inside `scope`) and picks the option. */
export async function selectOption(
  page: Page,
  scope: Page | Locator,
  label: string | RegExp,
  option: string | RegExp,
): Promise<void> {
  await scope.getByRole('combobox', { name: label, exact: typeof label === 'string' }).click();
  await page.getByRole('option', { name: option, exact: typeof option === 'string' }).click();
}

/**
 * Picks an ISO date in a DatePicker: opens the popover, moves month by month to the date and clicks the day
 * (the calendar marks days with data-day="YYYY-MM-DD").
 */
export async function pickDate(page: Page, trigger: Locator, iso: string): Promise<void> {
  await trigger.click();
  const pop = page.locator('[data-slot="popover-content"]').last();
  await expect(pop).toBeVisible();
  for (let i = 0; i < 240; i++) {
    const cell = pop.locator(`td[data-day="${iso}"]:not([data-outside])`);
    if ((await cell.count()) > 0) {
      await cell.locator('button').click();
      await expect(pop).toBeHidden();
      return;
    }
    const shown =
      (await pop.locator('td[data-day]:not([data-outside])').first().getAttribute('data-day')) ??
      '';
    const dir = shown.slice(0, 7) < iso.slice(0, 7) ? 'Next' : 'Previous';
    await pop.getByRole('button', { name: `Go to the ${dir} Month` }).click();
  }
  throw new Error(`pickDate: ${iso} not reached`);
}

/** Creates an item through the items page. */
export async function createItemViaUi(page: Page, item: ItemViaUi): Promise<void> {
  await page.goto(`${shellPath(page)}/items`);
  await page.getByRole('button', { name: 'Nuovo articolo' }).click();
  const sheet = page.getByRole('dialog');
  await sheet.getByLabel('Codice', { exact: true }).fill(item.code);
  await sheet.getByLabel('Nome (italiano)', { exact: true }).fill(item.name);
  await selectOption(page, sheet, 'Tipo', ITEM_TYPE_LABELS[item.type]);
  await selectOption(page, sheet, 'Unità di misura', UNIT_LABELS[item.unit] ?? item.unit);
  const vat = item.vatPercent.trim().replace(/\s*%$/, '');
  if (PRESET_VAT.has(vat)) {
    await selectOption(page, sheet, 'Aliquota IVA', `${vat}%`);
  } else {
    await selectOption(page, sheet, 'Aliquota IVA', 'Altra aliquota');
    await sheet.getByLabel('Aliquota personalizzata', { exact: true }).fill(vat);
  }
  if (vat === '0') await selectOption(page, sheet, 'Natura IVA', /^N4 /);
  const includes = sheet.getByRole('switch', { name: 'Prezzo IVA inclusa' });
  if ((await includes.getAttribute('aria-checked')) !== String(item.includesVat)) {
    await includes.click();
  }
  await sheet.getByRole('button', { name: 'Salva' }).click();
  await expect(page.getByText('Articolo creato.')).toBeVisible();
  await expect(sheet).toBeHidden();
}

/** Creates a draft transit price list with per-metre night rules per period through the editor. */
export async function createTransitPriceListViaUi(
  page: Page,
  opts: TransitPriceListViaUi,
): Promise<{ priceListId: string }> {
  const shell = shellPath(page);
  await page.goto(`${shell}/price-lists`);
  await page.getByRole('button', { name: 'Nuovo listino' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Codice', { exact: true }).fill(opts.code);
  await dialog.getByLabel('Nome (italiano)', { exact: true }).fill(`Transiti ${opts.year}`);
  await pickDate(page, dialog.getByLabel('Valido dal', { exact: true }), `${opts.year}-01-01`);
  await pickDate(page, dialog.getByLabel('Valido fino al', { exact: true }), `${opts.year}-12-31`);
  // Transit is the default contract type of the dialog.
  await expect(dialog.getByRole('checkbox', { name: 'Transito' })).toBeChecked();
  await dialog.getByRole('button', { name: 'Crea' }).click();
  await expect(page).toHaveURL(/\/price-lists\/[0-9a-f-]{36}$/);
  const priceListId = new URL(page.url()).pathname.split('/').pop()!;

  // Generale: the primary item of transits.
  await selectOption(
    page,
    page,
    'Articolo principale per Transito',
    new RegExp(`^${escapeRe(opts.itemCode)} `),
  );

  // Periodi: one season per period, one range each.
  await page.getByRole('tab', { name: /Periodi/ }).click();
  for (const [i, p] of opts.periods.entries()) {
    await page.getByRole('button', { name: 'Aggiungi periodo' }).click();
    const card = page.getByTestId('period-card').nth(i);
    await card.getByLabel('Codice', { exact: true }).fill(p.code);
    await card.getByLabel('Nome', { exact: true }).fill(p.code);
    await pickDate(page, card.getByLabel('Dalla notte del', { exact: true }), p.from);
    await pickDate(page, card.getByLabel('Alla notte del', { exact: true }), p.to);
  }

  // Fasce (optional).
  if (opts.bands?.length) {
    await page.getByRole('tab', { name: /Fasce/ }).click();
    for (const [i, b] of opts.bands.entries()) {
      await page.getByRole('button', { name: 'Aggiungi fascia' }).click();
      await page.getByLabel(`Codice della fascia ${i + 1}`, { exact: true }).fill(b.code);
      await page
        .getByLabel(`Lunghezza massima della fascia ${i + 1}`, { exact: true })
        .fill(b.maxMetres);
      if (b.category) {
        await selectOption(page, page, `Categoria della fascia ${i + 1}`, b.category);
      }
    }
  }

  // Regole: a per-metre night price per period.
  await page.getByRole('tab', { name: /Regole/ }).click();
  for (const [period, euro] of Object.entries(opts.perMeterEuro)) {
    await page.getByRole('button', { name: 'Aggiungi prezzo base' }).click();
    const sheet = page.getByRole('dialog');
    await sheet.getByLabel('Nome', { exact: true }).fill(`Transito ${period}`);
    await selectOption(page, sheet, 'Articolo', new RegExp(`^${escapeRe(opts.itemCode)} `));
    await sheet.getByLabel('Prezzo al metro', { exact: true }).fill(euro);
    await sheet.getByRole('button', { name: 'Periodi' }).click();
    await page.getByRole('checkbox', { name: period, exact: true }).check();
    await page.keyboard.press('Escape');
    await sheet.getByRole('button', { name: 'Applica' }).click();
    await expect(sheet).toBeHidden();
  }

  await page.getByRole('button', { name: 'Salva', exact: true }).click();
  await expect(page.getByText(/^Listino salvato/)).toBeVisible();
  return { priceListId };
}

/** Activates a draft price list from its editor (default effective date). */
export async function activatePriceListViaUi(page: Page, priceListId: string): Promise<void> {
  await page.goto(`${shellPath(page)}/price-lists/${priceListId}`);
  await page.getByRole('button', { name: 'Attiva', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: 'Attiva', exact: true }).click();
  await expect(page.getByText('Listino attivato.').first()).toBeVisible();
  // When predecessors were truncated the dialog stays open with the list.
  if (await dialog.isVisible()) {
    const close = dialog.getByRole('button', { name: 'Chiudi' });
    if (await close.isVisible()) await close.click();
  }
  await expect(page.getByText('Listino pubblicato')).toBeVisible();
}

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
