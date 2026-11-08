import { expect, test } from "@playwright/test";

import { archiveOpsHarbour, setupOpsHarbour } from "../support/occupancy";
import { login } from "../support/session";
import { setupYardCatalogue } from "../support/yard";

// Yard catalogue (m10-plan §14.1): the ANTIVEG package and its variables are listed; a formula with an unknown
// variable is refused on save with the issue shown inline, a valid one becomes version 2; a work type is created
// from its dialog.

let marina: string | null = null;
test.afterEach(async ({ page }) => {
  if (marina) await archiveOpsHarbour(page, marina);
  marina = null;
});

test("yard catalogue: package versions, formula issues and work types", async ({ page }) => {
  test.setTimeout(2 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 2 });
  marina = h.marinaId;
  await setupYardCatalogue(page, h.marinaId);

  await page.goto(`/admin/m/${h.marinaId}/yard/catalog`);
  const row = page.getByTestId("package-row").filter({ hasText: "ANTIVEG" });
  await expect(row).toContainText("v1");

  await page.getByRole("tab", { name: "Variabili" }).click();
  await expect(page.getByTestId("variable-row").filter({ hasText: "hull_area" })).toBeVisible();

  await page.getByRole("tab", { name: "Gruppi di lavorazioni" }).click();
  await row.getByRole("button", { name: "Modifica" }).click();
  const dialog = page.getByRole("dialog");
  const washQty = dialog.getByTestId("package-line").nth(1).getByLabel("Formula quantità");
  await washQty.fill("round(hull_area * nope, 1)");
  await dialog.getByRole("button", { name: "Salva nuova versione" }).click();
  await expect(dialog.getByText("Variabile sconosciuta: nope.")).toBeVisible();
  await washQty.fill("round(hull_area, 1)");
  await dialog.getByRole("button", { name: "Prova" }).nth(2).click();
  await expect(dialog.getByText("Quantità: 56,1")).toBeVisible();
  await dialog.getByRole("button", { name: "Salva nuova versione" }).click();
  await expect(dialog).toBeHidden();
  await expect(row).toContainText("v2");

  await page.getByRole("tab", { name: "Tipi di lavorazione" }).click();
  await page.getByRole("button", { name: "Nuovo tipo di lavorazione" }).click();
  const wt = page.getByRole("dialog");
  await wt.getByLabel("Codice").fill("HULL");
  await wt.getByLabel("Nome (italiano)").fill("Carena");
  await wt.getByRole("button", { name: "Salva" }).click();
  await expect(page.getByTestId("work-type-row").filter({ hasText: "HULL" })).toBeVisible();
});
