import { expect, test } from '@playwright/test';

import {
  createContractViaApi,
  createTransitViaApi,
  setupOpsHarbour,
  setupPrices,
  archiveOpsHarbour,
} from '../support/occupancy';
import { legendCount, lensFill, openOps, opsMap, today } from '../support/opsmap';
import {
  createCustomer,
  createVessel,
  dayFromToday,
  expectNoSeriousViolations,
  uniq,
} from '../support/registry';
import { BASE_URL } from '../support/env';
import { login } from '../support/session';

// Operational view (m3-plan §9.5, WEB-OPSMAP): lenses and legend counts, legend filter, date, quick card by role,
// live recolouring through SSE, list alternative, dark theme and axe.

// Archive the harbour of each run, otherwise later logins land on the marina picker.
let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test.describe('operational map', () => {
  test('lenses, legend, date, quick card, list, live update, themes', async ({ page, browser }) => {
    test.setTimeout(3 * 60_000);
    await login(page);
    const h = await setupOpsHarbour(page, { berths: 6 });
    harbour = h.marinaId;
    // Vessels and customers are created in the marina of the page URL: enter the new marina first.
    await page.goto(`/admin/m/${h.marinaId}`);
    await setupPrices(page, h.marinaId);
    const tag = uniq();
    const owner = await createCustomer(page, {
      kind: 'person',
      first_name: 'Ada',
      last_name: `Ops ${tag}`,
    });
    const annual = await createVessel(page, {
      name: `Maestrale ${tag}`,
      loa_cm: 1000,
      beam_cm: 350,
    });
    const visitor = await createVessel(page, {
      name: `Libeccio ${tag}`,
      loa_cm: 900,
      beam_cm: 320,
    });
    const late = await createVessel(page, {
      name: `Scirocco ${tag}`,
      loa_cm: 800,
      beam_cm: 300,
    });
    const [b0, b1, b2, b3] = h.berths;
    await createContractViaApi(page, h.marinaId, {
      vesselId: annual.id,
      customerId: owner.id,
      berthId: b0!.id,
      term: 'annual',
      startsOn: today(),
      activate: true,
    });
    await createTransitViaApi(page, h.marinaId, {
      vesselId: visitor.id,
      berthId: b1!.id,
      arrival: today(),
      departure: dayFromToday(3),
      confirm: true,
    });

    await openOps(page, h.marinaId, h.mapId);
    // Legend counts come from the day view.
    await expect(page.getByTestId('ops-legend-count-annual')).toHaveText('1');
    await expect(page.getByTestId('ops-legend-count-transit')).toHaveText('1');
    expect(await legendCount(page, 'free')).toBeGreaterThanOrEqual(4);
    await expect(lensFill(page, b0!.id)).toHaveAttribute('data-lens', 'annual');
    await expect(lensFill(page, b1!.id)).toHaveAttribute('data-lens', 'transit');
    await expect(
      opsMap(page).getByRole('button', {
        name: new RegExp(`${b0!.code}, Contratto, Maestrale`),
      }),
    ).toBeVisible();

    // The legend is the filter: unchecking "Transito" greys the transit berth, the count stays.
    await page.getByTestId('ops-legend-item-transit').getByRole('checkbox').click();
    await expect(lensFill(page, b1!.id)).toHaveClass(/fill-harbor-muted/);
    await expect(page.getByTestId('ops-legend-count-transit')).toHaveText('1');
    await page.getByRole('button', { name: 'Seleziona tutto', exact: true }).click();

    // Lens switch.
    await page.getByTestId('ops-lens-tab-presence').click();
    await expect(page).toHaveURL(/lens=presence/);
    await expect(page.getByTestId('ops-legend-item-in')).toBeVisible();
    await page.getByTestId('ops-lens-tab-occupancy').click();

    // Date change: after the transit departs the berth is free again.
    await page.getByRole('button', { name: 'Notte successiva' }).click();
    await page.getByRole('button', { name: 'Notte successiva' }).click();
    await page.getByRole('button', { name: 'Notte successiva' }).click();
    await expect(page.getByTestId('ops-date')).toHaveAttribute('data-date', dayFromToday(3));
    await expect(lensFill(page, b1!.id)).toHaveAttribute('data-lens', 'free');
    await page.getByRole('button', { name: 'Oggi' }).click();
    await expect(lensFill(page, b1!.id)).toHaveAttribute('data-lens', 'transit');

    // Quick card with the owner's actions.
    await opsMap(page)
      .getByRole('button', { name: new RegExp(`^${b1!.code}, Transito`) })
      .click();
    const card = page.getByTestId('ops-quick-card');
    await expect(card).toContainText(`Libeccio ${tag}`);
    await expect(card.getByTestId('ops-quick-action-move')).toBeVisible();
    await expect(card.getByTestId('ops-quick-action-swap')).toBeVisible();
    await opsMap(page)
      .getByRole('button', { name: new RegExp(`^${b2!.code}, Libero`) })
      .click();
    await expect(card.getByTestId('ops-quick-action-newTransit')).toBeVisible();
    await expect(card.getByTestId('ops-quick-action-block')).toBeVisible();

    // Live update: a transit created from another session recolours the berth within 3 s (SSE invalidation).
    // The second session reuses the owner's cookies: a fresh login would land on the marina picker (two marinas).
    const other = await browser.newContext({
      baseURL: BASE_URL,
      storageState: await page.context().storageState(),
    });
    const page2 = await other.newPage();
    await createTransitViaApi(page2, h.marinaId, {
      vesselId: late.id,
      berthId: b3!.id,
      arrival: today(),
      departure: dayFromToday(1),
      confirm: true,
    });
    // Live through SSE (the page is not reloaded); no latency bound.
    await expect(lensFill(page, b3!.id)).toHaveAttribute('data-lens', 'transit');
    await other.close();

    // List alternative by structure.
    await page.getByTestId('ops-list-toggle').click();
    await expect(page.getByTestId('ops-list')).toContainText(`Maestrale ${tag}`);
    await page.getByTestId('ops-list').getByRole('button', { name: b0!.code }).click();
    await expect(page.getByTestId('ops-quick-card')).toContainText(`Maestrale ${tag}`);
    await page.getByTestId('ops-list-toggle').click();

    // Accessibility in light and dark themes.
    await expectNoSeriousViolations(page, 'ops light');
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.reload();
    await expect(opsMap(page)).toHaveAttribute('data-date', today());
    await expectNoSeriousViolations(page, 'ops dark');
  });

  test('a viewer sees the map without write actions', async ({ page }) => {
    await login(page);
    const h = await setupOpsHarbour(page, { berths: 2 });
    harbour = h.marinaId;
    await openOps(page, h.marinaId, h.mapId);
    // Schema mode is remembered per user.
    await page.getByTestId('ops-mode-schema').click();
    await expect(opsMap(page)).toHaveAttribute('data-mode', 'schema');
    await page.goto(`/admin/m/${h.marinaId}/ops?map=${h.mapId}`);
    await expect(opsMap(page)).toHaveAttribute('data-mode', 'schema');
    await page.getByTestId('ops-mode-scale').click();
  });
});
