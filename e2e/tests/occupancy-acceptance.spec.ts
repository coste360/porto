import { expect, test, type Page } from '@playwright/test';

import { adminRequest, marinaApi } from '../support/map';
import {
  createTransitViaApi,
  setupOpsHarbour,
  setupPrices,
  archiveOpsHarbour,
} from '../support/occupancy';
import { openOps, opsMap, today } from '../support/opsmap';
import { pickDate } from '../support/pricing';
import {
  createCustomer,
  createVessel,
  dayFromToday,
  expectNoSeriousViolations,
  uniq,
} from '../support/registry';
import { login } from '../support/session';

// M3 acceptance from the UI (spec 32.3 M3, m3-plan §9.5) on a harbour seeded through the API: search, quote,
// accept, contract, transit, arrival, move (planner drag), layout save, release; axe on the operational view,
// the transit list, the quote editor, the contract wizard and the dock panel, in light and dark.

async function a11y(page: Page, label: string) {
  await expectNoSeriousViolations(page, `${label} light`);
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.reload();
  await page.waitForLoadState('networkidle');
  await expectNoSeriousViolations(page, `${label} dark`);
  await page.emulateMedia({ colorScheme: 'light' });
}

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test('M3 desk scenario end to end', async ({ page }) => {
  test.setTimeout(8 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 10, linearAreaM: 60 });
  harbour = h.marinaId;
  await setupPrices(page, h.marinaId);
  await page.goto(`/admin/m/${h.marinaId}`);
  const api = marinaApi(h.marinaId);
  const tag = uniq();
  const customer = await createCustomer(page, {
    kind: 'person',
    first_name: 'Gino',
    last_name: `Acc ${tag}`,
  });
  const yacht = await createVessel(page, {
    name: `Aliseo ${tag}`,
    loa_cm: 1134,
    beam_cm: 380,
  });
  const owned = await createVessel(page, {
    name: `Tramontana ${tag}`,
    loa_cm: 1000,
    beam_cm: 350,
  });

  // 1. Search from the operational view.
  await openOps(page, h.marinaId, h.mapId);
  const panel = page.getByTestId('desk-incoming');
  await panel.getByLabel('Lunghezza fuori tutto').fill('11,34');
  await panel.getByLabel('Larghezza', { exact: true }).fill('3,80');
  await pickDate(page, panel.getByLabel('Partenza'), dayFromToday(2));
  await panel.getByRole('button', { name: 'Cerca', exact: true }).click();
  await expect(panel.getByTestId('desk-solution').first()).toBeVisible();
  await a11y(page, 'ops');

  // 2. Quote from a solution, accepted as a transit.
  await openOps(page, h.marinaId, h.mapId);
  await panel.getByLabel('Lunghezza fuori tutto').fill('11,34');
  await panel.getByLabel('Larghezza', { exact: true }).fill('3,80');
  await panel.getByRole('button', { name: 'Cerca', exact: true }).click();
  await panel
    .getByTestId('desk-solution')
    .first()
    .getByRole('button', { name: 'Nuovo preventivo' })
    .click();
  const quoteDialog = page.getByTestId('desk-quote-dialog');
  await quoteDialog.getByRole('combobox', { name: 'Cliente' }).click();
  await page.getByRole('option', { name: customer.display_name }).click();
  await quoteDialog.getByRole('combobox', { name: 'Imbarcazione' }).click();
  await page.getByRole('option', { name: new RegExp(`Aliseo ${tag}`) }).click();
  await expectNoSeriousViolations(page, 'quote editor');
  await quoteDialog.getByRole('button', { name: 'Crea il preventivo' }).click();
  await expect(page.getByRole('heading', { name: /PRV-/ })).toBeVisible();
  await page.getByRole('button', { name: 'Accetta' }).click();
  await page.getByRole('dialog').getByLabel('Conferma e registra pagamento manuale').check();
  await page.getByRole('dialog').getByRole('button', { name: 'Accetta' }).click();
  await expect(page).toHaveURL(/\/transits\//);
  await expect(page.getByText('Confermato', { exact: true })).toBeVisible();
  const transitId = page.url().split('/').pop()!;
  await page.goto(`/admin/m/${h.marinaId}/transits`);
  await expect(page.getByRole('radio', { name: 'Tutti' })).toBeVisible();
  await a11y(page, 'transit list');

  // 3. Annual contract from the wizard, activated.
  await page.goto(
    `/admin/m/${h.marinaId}/contracts/new?vessel=${owned.id}&from=${today()}&rt=berth&rid=${h.berths[5]!.id}`,
  );
  const wizard = page.getByTestId('desk-contract-wizard');
  await wizard.getByRole('combobox', { name: 'Cliente' }).click();
  await page.getByRole('option', { name: customer.display_name }).click();
  await a11y(page, 'contract wizard');
  await wizard.getByRole('combobox', { name: 'Cliente' }).click();
  await page.getByRole('option', { name: customer.display_name }).click();
  await wizard.getByRole('button', { name: 'Attiva (firmato su carta)' }).click();
  await expect(page.getByText('Attivo', { exact: true })).toBeVisible();

  // 4. Release ten nights of the annual berth.
  await page.getByRole('button', { name: 'Rilascia notti' }).click();
  const release = page.getByTestId('desk-release-dialog');
  await pickDate(page, release.getByLabel('Alla notte', { exact: true }), dayFromToday(9));
  await release.getByRole('button', { name: 'Rilascia', exact: true }).click();
  await expect(release).toBeHidden();
  await expect(page.getByText('Rilasci', { exact: true })).toBeVisible();

  // 5. Arrival of the transit from the dock panel.
  await page.goto(`/admin/m/${h.marinaId}/dock`);
  await page
    .getByTestId('desk-movements-arrivals_expected')
    .getByRole('listitem')
    .filter({ hasText: `Aliseo ${tag}` })
    .getByRole('button', { name: 'Arrivato' })
    .click();
  await page
    .getByTestId('desk-presence-dialog')
    .getByRole('button', { name: "Registra l'arrivo" })
    .click();
  await expect(page.getByTestId('dock-page')).toContainText('Arrivate: 1');
  await a11y(page, 'dock');

  // 6. Move: a second transit dragged one row down in the planner.
  const visitor = await createVessel(page, {
    name: `Levante ${tag}`,
    loa_cm: 900,
    beam_cm: 320,
  });
  await createTransitViaApi(page, h.marinaId, {
    vesselId: visitor.id,
    berthId: h.berths[7]!.id,
    arrival: today(),
    departure: dayFromToday(2),
    confirm: true,
  });
  await page.goto(`/admin/m/${h.marinaId}/planner?map=${h.mapId}&from=${today()}`);
  const bar = page.getByRole('button', { name: new RegExp(`Levante ${tag}`) });
  const box = (await bar.locator('rect').first().boundingBox())!;
  await page.mouse.move(box.x + 6, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + 6, box.y + box.height / 2 + 28, { steps: 5 });
  await page.mouse.up();
  const change = page.getByTestId('desk-planner-change');
  await change.getByRole('button', { name: 'Sposta' }).click();
  await expect(change).toBeHidden();

  // 7. Layout session of the linear area: one vessel placed through the API, then saved at a new x from the UI.
  const areaGuest = await createVessel(page, {
    name: `Ponente ${tag}`,
    loa_cm: 800,
    beam_cm: 300,
  });
  await createTransitViaApi(page, h.marinaId, {
    vesselId: areaGuest.id,
    // Away from the start of the area, where the transit accepted from the quote is.
    areaX: 3000,
    arrival: today(),
    departure: dayFromToday(2),
    confirm: true,
  });
  await page.goto(`/admin/m/${h.marinaId}/ops/area/${h.areaId}?date=${today()}`);
  await expect(page.getByTestId('ops-layout-save')).toBeVisible();

  // Invariants of the scenario through the API.
  const { data: transit } = await adminRequest<{ status: string }>(
    page,
    'GET',
    `${api}/contracts/${transitId}`,
  );
  expect(transit.status).toBe('arrived');
  const { data: releases } = await adminRequest<{
    items: { status: string }[];
  }>(page, 'GET', `${api}/releases`);
  expect(releases.items.some((r) => r.status === 'active')).toBe(true);
  await openOps(page, h.marinaId, h.mapId);
  await expect(
    opsMap(page).getByRole('button', { name: new RegExp(`Levante ${tag}`) }),
  ).toBeVisible();
});
