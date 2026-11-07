import { expect, test, type Page } from '@playwright/test';

import { adminRequest, marinaApi } from '../support/map';
import { login } from '../support/session';

// Coverage check (spec 12.5, m2-plan §8.4): a complete draft has no gaps; removing the BASSA rule from the
// draft makes the (transit, BASSA) combinations appear as gaps, with a link back to the rules.

type Json = Record<string, unknown>;
const uniq = () => Date.now().toString(36).slice(-5);
const YEAR = new Date().getFullYear() + 1;

async function createFreshMarina(page: Page): Promise<string> {
  const me = await adminRequest<{ organizations: { id: string }[] }>(
    page,
    'GET',
    '/api/admin/v1/auth/me',
  );
  const slug = `cop-${uniq()}`;
  const marina = await adminRequest<{ id: string }>(
    page,
    'POST',
    `/api/admin/v1/organizations/${me.data.organizations[0]!.id}/marinas`,
    { body: { name: `Marina ${slug}`, slug, timezone: 'Europe/Rome' } },
  );
  return marina.data.id;
}

test('coverage shows the gap left by a rule removed from a draft', async ({ page }) => {
  await login(page);
  const marinaId = await createFreshMarina(page);
  try {
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
    const code = `C-${YEAR}`;
    const created = await adminRequest<Json & { id: string; version: number; settings: Json }>(
      page,
      'POST',
      `${api}/price-lists`,
      {
        body: {
          code,
          name: { it: 'Copertura' },
          valid_from: `${YEAR}-01-01`,
          valid_to: `${YEAR}-12-31`,
          contract_types: ['transit'],
        },
      },
    );
    const listId = created.data.id;
    const rule = (key: string, period: string, cents: number) => ({
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
    const document = (rules: Json[]) => ({
      code,
      name: { it: 'Copertura' },
      valid_from: `${YEAR}-01-01`,
      valid_to: `${YEAR}-12-31`,
      contract_types: ['transit'],
      settings: {
        ...created.data.settings,
        primary_items: { transit: item.data.id },
      },
      periods: [
        {
          code: 'BASSA',
          name: { it: 'Bassa' },
          kind: 'season',
          ranges: [{ from: `${YEAR}-01-01`, to: `${YEAR}-06-30` }],
        },
        {
          code: 'ALTA',
          name: { it: 'Alta' },
          kind: 'season',
          ranges: [{ from: `${YEAR}-07-01`, to: `${YEAR}-12-31` }],
        },
      ],
      bands: [
        { code: 'L10', max_cm: 1000, category: 'L10' },
        { code: 'L12', max_cm: 1200, category: 'L12' },
      ],
      rules,
    });
    // Saved rows of a later save carry their ids: the second save re-reads the document first.
    const withKeys = (d: ReturnType<typeof document>) => ({
      ...d,
      periods: d.periods.map((p) => ({ ...p, key: p.code.toLowerCase() })),
      bands: d.bands.map((b) => ({ ...b, key: b.code.toLowerCase() })),
    });
    const first = await adminRequest<{
      document: Json & {
        version: number;
        rules: Json[];
        periods: Json[];
        bands: Json[];
      };
    }>(page, 'PUT', `${api}/price-lists/${listId}`, {
      ifMatch: created.data.version,
      body: withKeys(document([rule('r-bassa', 'BASSA', 400), rule('r-alta', 'ALTA', 750)])),
    });

    await page.goto(`/admin/m/${marinaId}/price-lists/${listId}/coverage`);
    await expect(
      page.getByRole('heading', { name: `Copertura del listino ${code}` }),
    ).toBeVisible();
    await expect(page.getByText('Ogni combinazione ha un prezzo')).toBeVisible();
    await expect(page.getByTestId('coverage-gaps')).toHaveText('0');

    // Remove the BASSA rule from the draft.
    const saved = first.data.document;
    const { seq: _a, ...alta } = saved.rules.find((r) => r.name === 'Transito ALTA')!;
    await adminRequest(page, 'PUT', `${api}/price-lists/${listId}`, {
      ifMatch: saved.version,
      body: { ...document([alta]), periods: saved.periods, bands: saved.bands },
    });

    await page.reload();
    await expect(page.getByTestId('coverage-gaps')).not.toHaveText('0');
    const gaps = page.getByTestId('coverage-gap');
    await expect(gaps.first()).toContainText('BASSA');
    await expect(gaps.filter({ hasText: 'ALTA' })).toHaveCount(0);
    await expect(gaps.first()).toContainText('Transito');
    await expect(gaps.first().getByRole('link', { name: 'Apri regole' })).toHaveAttribute(
      'href',
      `/admin/m/${marinaId}/price-lists/${listId}#rules`,
    );

    // The period filter keeps only the BASSA rows.
    await page.getByLabel('Periodo', { exact: true }).click();
    await page.getByRole('option', { name: 'BASSA' }).click();
    await expect(gaps.filter({ hasText: 'BASSA' })).toHaveCount(await gaps.count());
  } finally {
    await adminRequest(page, 'POST', `/api/admin/v1/marinas/${marinaId}/archive`, {
      expectStatus: 204,
    });
  }
});
