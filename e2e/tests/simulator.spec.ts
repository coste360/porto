import { expect, test, type Page } from '@playwright/test';

import { adminRequest, marinaApi } from '../support/map';
import { expectNoSeriousViolations } from '../support/registry';
import { OWNER } from '../support/env';
import { fillLogin, login } from '../support/session';

// Simulator, matrix and print pages against a transit list set up through the API in a marina of its own
// (m2-plan §8.4): no-price reason, promo not applicable, multihull modifier in the trace, print page, axe.

type Json = Record<string, unknown>;
const uniq = () => Date.now().toString(36).slice(-5);
const YEAR = new Date().getFullYear() + 1;

/** Same encoding as the web `encodeProbe`: base64url of the JSON request. */
const probeUrl = (marinaId: string, probe: Json) =>
  `/admin/m/${marinaId}/pricing/simulator?probe=${Buffer.from(JSON.stringify(probe)).toString('base64url')}`;

async function createFreshMarina(page: Page): Promise<string> {
  const me = await adminRequest<{ organizations: { id: string }[] }>(
    page,
    'GET',
    '/api/admin/v1/auth/me',
  );
  const slug = `sim-${uniq()}`;
  const marina = await adminRequest<{ id: string }>(
    page,
    'POST',
    `/api/admin/v1/organizations/${me.data.organizations[0]!.id}/marinas`,
    { body: { name: `Marina ${slug}`, slug, timezone: 'Europe/Rome' } },
  );
  return marina.data.id;
}

/** Item TRN-NTT (22 % included) and an active transit list: BASSA 4,00 €/m, ALTA 7,50 €/m, multihull +50 %. */
async function activeTransitList(
  page: Page,
  marinaId: string,
): Promise<{ id: string; code: string }> {
  const api = marinaApi(marinaId);
  const classes = await adminRequest<{ items: { id: string }[] }>(
    page,
    'GET',
    `${api}/revenue-classes`,
  );
  const item = await adminRequest<{ id: string }>(page, 'POST', `${api}/items`, {
    body: {
      code: 'TRN-NTT',
      name: { it: 'Transito notte' },
      item_type: 'berth_fee',
      unit: 'night',
      vat_rate_bp: 2200,
      price_includes_vat: true,
      revenue_class_id: classes.data.items[0]!.id,
    },
  });
  const code = `T-${YEAR}-${uniq().toUpperCase()}`;
  const created = await adminRequest<Json & { id: string; version: number; settings: Json }>(
    page,
    'POST',
    `${api}/price-lists`,
    {
      body: {
        code,
        name: { it: 'Transiti' },
        valid_from: `${YEAR}-01-01`,
        valid_to: `${YEAR}-12-31`,
        contract_types: ['transit'],
      },
    },
  );
  const doc = created.data;
  const perMeter = (key: string, period: string, cents: number) => ({
    key,
    stage: 'base',
    name: `Transito ${period}`,
    active: true,
    item_id: item.data.id,
    basis: 'per_meter',
    unit: 'night',
    unit_cents: cents,
    scope: { periods: [period] },
  });
  const saved = await adminRequest<{ document: { version: number } }>(
    page,
    'PUT',
    `${api}/price-lists/${doc.id}`,
    {
      ifMatch: doc.version,
      body: {
        code,
        name: { it: 'Transiti' },
        valid_from: `${YEAR}-01-01`,
        valid_to: `${YEAR}-12-31`,
        contract_types: ['transit'],
        settings: { ...doc.settings, primary_items: { transit: item.data.id } },
        periods: [
          {
            key: 'bassa',
            code: 'BASSA',
            name: { it: 'Bassa' },
            kind: 'season',
            ranges: [{ from: `${YEAR}-01-01`, to: `${YEAR}-06-30` }],
          },
          {
            key: 'alta',
            code: 'ALTA',
            name: { it: 'Alta' },
            kind: 'season',
            ranges: [{ from: `${YEAR}-07-01`, to: `${YEAR}-12-31` }],
          },
        ],
        bands: [
          { key: 'l10', code: 'L10', max_cm: 1000, category: 'L10' },
          { key: 'l12', code: 'L12', max_cm: 1200, category: 'L12' },
        ],
        rules: [
          perMeter('r-bassa', 'BASSA', 400),
          perMeter('r-alta', 'ALTA', 750),
          {
            key: 'r-multi',
            stage: 'modifier',
            name: 'Multiscafo +50%',
            active: true,
            stackable: true,
            target_item_types: ['berth_fee'],
            conditions: { hull_types: ['catamaran', 'trimaran'] },
            effect: { percent_bp: 5000 },
          },
        ],
      },
    },
  );
  await adminRequest(page, 'POST', `${api}/price-lists/${doc.id}/activate`, {
    body: {},
    ifMatch: saved.data.document.version,
  });
  return { id: doc.id, code };
}

async function signIn(page: Page) {
  await page.goto('/admin/login');
  await fillLogin(page, OWNER.email, OWNER.password);
  await page.waitForURL((url) => !url.pathname.endsWith('/login'));
}

const stay = (extra: Json = {}): Json => ({
  contract_type: 'transit',
  arrival: `${YEAR}-07-10`,
  departure: `${YEAR}-07-13`,
  vessel: { loa_cm: 1134, beam_cm: 380, hull_type: 'monohull_motor' },
  category: 'L12',
  ...extra,
});

test.describe('pricing simulator and matrix', () => {
  let marinaId = '';
  let list = { id: '', code: '' };

  test.beforeEach(async ({ page }) => {
    if (marinaId) {
      // With the extra marina the owner lands on the marina picker, not on a shell.
      await signIn(page);
      return;
    }
    await login(page);
    {
      marinaId = await createFreshMarina(page);
      list = await activeTransitList(page, marinaId);
    }
  });

  test.afterAll(async ({ browser }) => {
    if (!marinaId) return;
    const page = await browser.newPage();
    await signIn(page);
    await adminRequest(page, 'POST', `/api/admin/v1/marinas/${marinaId}/archive`, {
      expectStatus: 204,
    });
    await page.close();
  });

  test('prices a stay and explains how', async ({ page }) => {
    await page.goto(probeUrl(marinaId, stay()));
    await expect(page.getByTestId('breakdown-total')).toHaveText('256,50 €');
    await expect(page.getByTestId('breakdown-resolved')).toContainText(list.code);
    await expect(page.getByTestId('breakdown-trace')).toContainText('Regola vincente');
  });

  test('shows the no-price reason', async ({ page }) => {
    // Two years later no list is active for transit: the reason comes from the problem details.
    await page.goto(
      probeUrl(marinaId, stay({ arrival: `${YEAR + 2}-07-10`, departure: `${YEAR + 2}-07-11` })),
    );
    await expect(page.getByTestId('simulator-no-price')).toBeVisible();
    await expect(page.getByTestId('no-price-reason')).toHaveText(
      'Nessun listino attivo per questa data e tipo di contratto',
    );
  });

  test('says when the promo code does not apply', async ({ page }) => {
    await page.goto(probeUrl(marinaId, stay({ promo_code: 'NOPE' })));
    await expect(page.getByTestId('simulator-promo')).toContainText(
      'Il codice promozionale non è applicabile',
    );
  });

  test('shows the multihull modifier in the trace', async ({ page }) => {
    await page.goto(
      probeUrl(
        marinaId,
        stay({
          vessel: { loa_cm: 1134, beam_cm: 600, hull_type: 'catamaran' },
        }),
      ),
    );
    // 256,50 € + 50 % = 384,75 €.
    await expect(page.getByTestId('breakdown-total')).toHaveText('384,75 €');
    const trace = page.getByTestId('breakdown-trace');
    await expect(trace).toContainText('Maggiorazione o riduzione applicata');
    await expect(trace).toContainText('Multiscafo +50%');
    await expect(page.getByText(/Multiscafo: le maggiorazioni/)).toBeVisible();
  });

  test('renders the printable matrix', async ({ page }) => {
    await page.goto(`/admin/m/${marinaId}/price-lists/${list.id}/matrix/print`);
    const sheet = page.getByTestId('matrix-print');
    await expect(sheet.getByRole('heading', { name: `Listino ${list.code}` })).toBeVisible();
    await expect(sheet.getByTestId('print-validity')).toHaveText(
      `Valido dal 01/01/${YEAR} al 31/12/${YEAR}`,
    );
    await expect(sheet.getByRole('heading', { name: 'Transito' })).toBeVisible();
    // Columns follow the period codes (ALTA, BASSA): look the L12 row up by its header.
    await expect(sheet.getByRole('row', { name: /^L12/ })).toContainText('90,00 €');
    await expect(sheet.getByAltText('Porto').first()).toBeVisible();
  });

  for (const scheme of ['light', 'dark'] as const) {
    test(`has no serious axe findings on simulator and matrix (${scheme})`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme });
      await page.goto(probeUrl(marinaId, stay()));
      await expect(page.getByTestId('breakdown-total')).toHaveText('256,50 €');
      await expectNoSeriousViolations(page, `simulator/${scheme}`);
      await page.goto(`/admin/m/${marinaId}/price-lists/${list.id}/matrix`);
      await expect(page.getByRole('button', { name: /^L12 × ALTA: 90,00 €/ })).toBeVisible();
      await expectNoSeriousViolations(page, `matrix/${scheme}`);
    });
  }
});
