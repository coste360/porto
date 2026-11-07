import { expect, test, type Page } from '@playwright/test';

import { adminRequest, addPierThroughApi, createMap, getMap, marinaApi } from '../support/map';
import {
  activatePriceListViaUi,
  createItemViaUi,
  createTransitPriceListViaUi,
} from '../support/pricing';
import { login } from '../support/session';

// Spec 32.3 M2 (m2-plan §8.4): a transit list built in the UI prices a stay in the simulator, the matrix is
// computed by the engine, and a matrix cell opened in the simulator gives the same price (INV-23).
//
// Hand-computed prices (VAT included, 22 %):
//   simulator: 11,34 m rounded up to the decimetre = 11,40 m × 7,50 €/m × 3 nights = 256,50 €;
//   matrix L12 × ALTA: band probe 12,00 m × 7,50 €/m × 1 night = 90,00 €.

const uniq = () => Date.now().toString(36).slice(-5);
/** Next calendar year: the stay must be in the list's validity and the calendar must reach it quickly. */
const YEAR = new Date().getFullYear() + 1;

type Json = Record<string, unknown>;

/** A marina of its own (fresh price lists, no overlap with other specs); archived at the end. */
async function createFreshMarina(page: Page): Promise<string> {
  const me = await adminRequest<{ organizations: { id: string }[] }>(
    page,
    'GET',
    '/api/admin/v1/auth/me',
  );
  const orgId = me.data.organizations[0]!.id;
  const slug = `prezzi-${uniq()}`;
  const marina = await adminRequest<{ id: string }>(
    page,
    'POST',
    `/api/admin/v1/organizations/${orgId}/marinas`,
    { body: { name: `Marina prezzi ${slug}`, slug, timezone: 'Europe/Rome' } },
  );
  return marina.data.id;
}

/** A pier of 12 m berths with price class L12, then the categories synced from the classes in use. */
async function l12Berths(page: Page, marinaId: string) {
  const map = await createMap(page, marinaId, 'Pontili');
  await addPierThroughApi(page, marinaId, map.id, {
    code: 'A',
    origin: { x: 10_000, y: 5_000 },
    berthsPerSide: 2,
    keyPrefix: 'a',
  });
  const doc = await getMap(page, marinaId, map.id);
  const { id: _id, version, updated_at: _u, ...rest } = doc as typeof doc & { updated_at: string };
  await adminRequest(page, 'PUT', `${marinaApi(marinaId)}/maps/${map.id}`, {
    body: {
      ...rest,
      berths: doc.berths.map((b) => ({ ...b, price_class: 'L12' })),
    },
    ifMatch: version,
  });
  await adminRequest(page, 'POST', `${marinaApi(marinaId)}/berth-categories/sync`);
}

/** Adds the length bands L10/L12 (linked to their categories) to the draft, before activation. */
async function addBands(page: Page, marinaId: string, priceListId: string) {
  const path = `${marinaApi(marinaId)}/price-lists/${priceListId}`;
  const { data: doc } = await adminRequest<Json & { version: number; rules: Json[] }>(
    page,
    'GET',
    path,
  );
  const save = {
    code: doc.code,
    name: doc.name,
    valid_from: doc.valid_from,
    valid_to: doc.valid_to,
    contract_types: doc.contract_types,
    settings: doc.settings,
    periods: doc.periods,
    bands: [
      { key: 'L10', code: 'L10', max_cm: 1000, category: 'L10' },
      { key: 'L12', code: 'L12', max_cm: 1200, category: 'L12' },
    ],
    rules: doc.rules.map(({ seq: _seq, ...r }) => r),
  };
  await adminRequest(page, 'PUT', path, { body: save, ifMatch: doc.version });
}

/** Picks a date in a DatePicker: opens it, moves month by month, clicks the day, waits for it to close. */
async function pickDate(page: Page, label: string, iso: string) {
  await page.getByLabel(label, { exact: true }).click();
  const calendar = page.getByRole('dialog');
  const day = calendar.locator(`td[data-day="${iso}"]:not([data-outside]) button`);
  for (let i = 0; i < 36 && (await day.count()) === 0; i++) {
    // The calendar is not localised: react-day-picker's own label.
    await calendar.getByRole('button', { name: 'Go to the Next Month' }).click();
  }
  await day.click();
  await expect(calendar).toBeHidden();
}

test('M2: transit list → simulator 256,50 € → matrix cell 90,00 € = its simulation', async ({
  page,
}) => {
  test.slow();
  await login(page);
  const marinaId = await createFreshMarina(page);
  try {
    await page.goto(`/admin/m/${marinaId}`);
    await expect(page.getByRole('button', { name: /Menu utente/ })).toBeVisible();
    await l12Berths(page, marinaId);

    // 1. Item and transit list through the UI, bands through the API, activation through the UI.
    await createItemViaUi(page, {
      code: 'TRN-NTT',
      name: 'Transito notte',
      type: 'berth_fee',
      unit: 'night',
      vatPercent: '22',
      includesVat: true,
    });
    const { priceListId } = await createTransitPriceListViaUi(page, {
      code: `T-${YEAR}`,
      year: YEAR,
      itemCode: 'TRN-NTT',
      periods: [
        { code: 'BASSA', from: `${YEAR}-01-01`, to: `${YEAR}-06-30` },
        { code: 'ALTA', from: `${YEAR}-07-01`, to: `${YEAR}-12-31` },
      ],
      perMeterEuro: { BASSA: '4,00', ALTA: '7,50' },
    });
    await addBands(page, marinaId, priceListId);
    await activatePriceListViaUi(page, priceListId);

    // 2. Simulator: 11,34 m monohull, 3 nights in ALTA, category L12, list active on the arrival night.
    await page.goto(`/admin/m/${marinaId}/pricing/simulator`);
    await expect(page.getByRole('heading', { name: 'Simulatore di prezzo' })).toBeVisible();
    await pickDate(page, 'Arrivo', `${YEAR}-07-10`);
    await pickDate(page, 'Partenza', `${YEAR}-07-13`);
    await expect(page.getByTestId('simulator-nights')).toHaveText('Notti: 3');
    await page.getByLabel('Lunghezza fuori tutto').fill('11,34');
    await page.getByLabel('Larghezza', { exact: true }).fill('3,80');
    await page.getByLabel('Categoria del posto').click();
    await page.getByRole('option', { name: 'L12', exact: true }).click();
    await page.getByRole('button', { name: 'Calcola' }).click();
    await expect(page.getByTestId('breakdown-total')).toHaveText('256,50 €');
    await expect(page.getByTestId('breakdown-trace')).toContainText('Lunghezza arrotondata');

    // 3. Matrix (transit): L12 × ALTA is one night of the band probe.
    await page.goto(`/admin/m/${marinaId}/price-lists/${priceListId}/matrix`);
    const cell = page.getByRole('button', { name: /^L12 × ALTA: 90,00 €/ });
    await expect(cell).toBeVisible();

    // 4. The cell opens the simulator prefilled with its probe and shows the same price.
    await cell.click();
    await expect(page).toHaveURL(/\/pricing\/simulator\?probe=/);
    await expect(page.getByTestId('breakdown-total')).toHaveText('90,00 €');
    await expect(page.getByLabel('Lunghezza fuori tutto')).toHaveValue('12,00');

    // 5. Through the API: every matrix cell of every contract type equals the simulation of its probe.
    const api = marinaApi(marinaId);
    const { data: doc } = await adminRequest<{ contract_types: string[] }>(
      page,
      'GET',
      `${api}/price-lists/${priceListId}`,
    );
    let compared = 0;
    for (const contractType of doc.contract_types) {
      const { data: m } = await adminRequest<{
        rows: { key: string }[];
        columns: { key: string }[];
        cells: {
          row: number;
          col: number;
          total_cents?: number;
          probe: Json;
        }[];
      }>(
        page,
        'GET',
        `${api}/price-lists/${priceListId}/matrix?contract_type=${contractType}&rows=bands`,
      );
      for (const c of m.cells) {
        if (c.total_cents === undefined) continue;
        const sim = await adminRequest<{ total_cents: number }>(
          page,
          'POST',
          `${api}/pricing/simulate`,
          {
            body: c.probe,
          },
        );
        expect(
          sim.data.total_cents,
          `${contractType} ${m.rows[c.row]?.key} × ${m.columns[c.col]?.key}`,
        ).toBe(c.total_cents);
        if (
          contractType === 'transit' &&
          m.rows[c.row]?.key === 'L12' &&
          m.columns[c.col]?.key === 'ALTA'
        ) {
          expect(c.total_cents).toBe(9000);
        }
        compared++;
      }
    }
    expect(compared).toBeGreaterThan(0);
  } finally {
    await adminRequest(page, 'POST', `/api/admin/v1/marinas/${marinaId}/archive`, {
      expectStatus: 204,
    });
  }
});
