import { expect, test } from "@playwright/test";

import { archiveOpsHarbour, setupOpsHarbour } from "../support/occupancy";
import { login } from "../support/session";
import { setupYardCatalogue } from "../support/yard";

// Guided yard quote (m10-plan §4.2, §4.4, acceptance criterion 1): the antifouling job for a 12 m boat in the
// wizard without typing any price; the summary shows the §4.4 totals and the created quote keeps them.

let marina: string | null = null;
test.afterEach(async ({ page }) => {
  if (marina) await archiveOpsHarbour(page, marina);
  marina = null;
});

test("yard quote: the antifouling job of a 12 m boat in the wizard", async ({ page }) => {
  test.setTimeout(2 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 2 });
  marina = h.marinaId;
  const w = await setupYardCatalogue(page, h.marinaId);

  await page.goto(`/admin/m/${h.marinaId}/yard/quotes`);
  await page.getByRole("link", { name: "Nuovo preventivo" }).click();
  await page.getByLabel("Cerca cliente").fill(w.customerName);
  await page.getByRole("button", { name: new RegExp(w.customerName) }).click();
  await page.getByLabel("Cerca unità").fill(w.vesselName);
  await page.getByRole("button", { name: new RegExp(w.vesselName) }).click();
  await page.getByRole("button", { name: "Continua" }).click();

  await page.getByLabel(/ANTIVEG/).check();
  await page.getByRole("button", { name: "Continua" }).click();
  await expect(page.getByLabel(/layers|Mani|layers/)).toHaveValue("2");
  await page.getByRole("button", { name: "Continua" }).click();

  await expect(page.getByTestId("yard-quote-line")).toHaveCount(6);
  await expect(page.getByTestId("total-net")).toContainText("1.702,60");
  await expect(page.getByTestId("total-vat")).toContainText("374,57");
  await expect(page.getByTestId("total-gross")).toContainText("2.077,17");
  await expect(page.getByTestId("total-deposit")).toContainText("623,15");
  await expect(page.getByTestId("manual-badge")).toHaveCount(0);
  await page.getByRole("button", { name: "Continua" }).click();
  await page.getByRole("button", { name: "Crea preventivo" }).click();

  await expect(page).toHaveURL(/\/yard\/quotes\/[0-9a-f-]+$/);
  await expect(page.getByTestId("yard-quote-status")).toHaveText("Bozza");
  await expect(page.getByTestId("total-gross")).toContainText("2.077,17");
});
