import { expect, test } from "@playwright/test";

import { login } from "../support/session";
import { expectNoSeriousViolations } from "../support/registry";

// Manager dashboard (spec 21.2, m7-plan §4, §6, §11.6): the tiles replace the overview placeholder; a tile opens
// its report with the same period and the report's "Totale" equals the tile value (INV-62).

test("dashboard tile opens its report with the same period and total", async ({
  page,
}) => {
  await login(page);
  const dashboard = page.getByTestId("manager-dashboard");
  await expect(dashboard).toBeVisible();
  await expect(
    page.getByText("La panoramica operativa arriverà nelle prossime versioni."),
  ).toHaveCount(0);

  const tile = dashboard.locator(
    '[data-testid="dashboard-tile"][data-tile="production"]',
  );
  await expect(tile).toBeVisible();
  const value = tile.getByTestId("tile-value");
  const text = (await value.textContent())?.trim() ?? "";
  expect(text).toMatch(/€/);

  // The seams of M10/M11 are listed as not available yet.
  await expect(page.getByTestId("dashboard-seams")).toContainText(
    "Disponibile con M10",
  );

  await value.click();
  await expect(page).toHaveURL(
    /\/reports\/production\?.*from=\d{4}-01-01.*to=\d{4}-\d{2}-\d{2}/,
  );
  await expect(page.getByTestId("report-total-row")).toContainText(text);
});

test("dashboard period presets and comparison controls", async ({ page }) => {
  await login(page);
  const dashboard = page.getByTestId("manager-dashboard");
  await expect(dashboard.getByTestId("dashboard-tiles")).toBeVisible();
  const asked = page.waitForRequest(
    (r) =>
      r.url().includes("/dashboard?") && /from=\d{4}-\d{2}-01/.test(r.url()),
  );
  await page.getByRole("combobox", { name: "Periodo" }).click();
  await page.getByRole("option", { name: "Mese in corso" }).click();
  await asked;
  const occupancy = dashboard.locator(
    '[data-testid="dashboard-tile"][data-tile="occupancy"]',
  );
  await expect(occupancy.getByTestId("tile-value")).toContainText(/%|—/);

  await page.getByRole("combobox", { name: "Confronto" }).click();
  await page.getByRole("option", { name: "Nessun confronto" }).click();
  await expect(page.getByTestId("tile-previous")).toHaveCount(0);
  await expectNoSeriousViolations(page, "manager dashboard");
});
