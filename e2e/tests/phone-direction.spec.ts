import { expect, test } from "@playwright/test";

import { archiveOpsHarbour } from "../support/occupancy";
import { expectPhoneLayout, loginOnPhone, seedDockDay } from "../support/phone";
import { expectNoSeriousViolations } from "../support/registry";

// Direction phone screen (spec 31.3 Direzione, m6b-plan §6.3, §10.2, m7-plan §10.1): dashboard tiles with today's
// counts, the day map, deadlines of the next 30 days; and the phone landing after sign-in.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test("sign-in on a phone lands on the phone screen", async ({ page }) => {
  await loginOnPhone(page);
  // The owner holds presence.write: the dock screen (the direction screen is one tap away in Altro).
  await expect(page).toHaveURL(/\/admin\/m\/[^/]+\/dock/);
  await page.getByRole("tab", { name: "Altro" }).click();
  await page.getByTestId("dock-more-direction").click();
  await expect(page.getByTestId("direction-page")).toBeVisible();
  // "Versione completa" opens the full shell and stays there.
  await page.getByTestId("phone-full-version").click();
  // On a phone the full shell keeps its sidebar (and the user menu) behind the sidebar toggle.
  await expect(page).toHaveURL(/\/admin\/m\/[^/]+\/?$/);
  await expect(page.getByTestId("phone-shell")).toHaveCount(0);
});

test("direction screen: dashboard, map, deadlines", async ({ page }) => {
  test.setTimeout(3 * 60_000);
  await loginOnPhone(page);
  const day = await seedDockDay(page);
  harbour = day.marinaId;
  await page.goto(`/admin/m/${day.marinaId}/direction`);
  const screen = page.getByTestId("direction-page");
  await expect(screen).toBeVisible();
  await expectPhoneLayout(page, "direction-page");
  // Dashboard tiles of the month in one column; a tap opens the report (AppShell) without horizontal scroll.
  const tiles = page.getByTestId("direction-tiles");
  await expect(tiles.getByTestId("dashboard-tile").first()).toBeVisible();
  await expectPhoneLayout(page, "direction-page");
  // Arrivals today: the transit and the annual contract starting today.
  await expect(page.getByTestId("direction-count").first()).toContainText("2");

  await screen.getByRole("tab", { name: "Mappa" }).click();
  await expect(page.getByTestId("phone-map-canvas")).toBeVisible();

  await screen.getByRole("tab", { name: "Scadenze" }).click();
  await expect(
    page
      .getByTestId("direction-deadlines")
      .or(page.getByText("Nessuna scadenza nei prossimi 30 giorni."))
      .first(),
  ).toBeVisible();
  await page.getByRole("link", { name: "Apri la pagina completa" }).click();
  await expect(page).toHaveURL(/\/deadlines/);

  await page.goto(`/admin/m/${day.marinaId}/direction`);
  await expectNoSeriousViolations(page, "phone direction");

  await page
    .getByTestId("direction-tiles")
    .locator('[data-testid="dashboard-tile"][data-tile="occupancy"]')
    .getByTestId("tile-value")
    .click();
  await expect(page).toHaveURL(/\/reports\/occupancy\?.*from=\d{4}-\d{2}-01/);
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(page.viewportSize()!.width);
});
