import { expect, test } from "@playwright/test";

import { adminRequest, marinaApi } from "../support/map";
import { archiveOpsHarbour, setupOpsHarbour } from "../support/occupancy";
import { login } from "../support/session";

// Maintenance board (m9-plan §8, §12.2): a task created from the board on the map, opened, started and completed
// with an outcome; the server records it done once.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test("maintenance: create, start and complete a task", async ({ page }) => {
  test.setTimeout(2 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 2 });
  harbour = h.marinaId;
  const title = `Verifica catena ${Date.now()}`;

  await page.goto(`/admin/m/${h.marinaId}/maintenance`);
  await page.getByRole("button", { name: "Nuovo intervento" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Titolo").fill(title);
  await dialog.getByRole("button", { name: "Crea intervento" }).click();
  await expect(dialog).toBeHidden();

  const row = page
    .getByTestId("maintenance-task-row")
    .filter({ hasText: title });
  await expect(row).toBeVisible();
  await row.getByRole("link").first().click();
  await expect(page).toHaveURL(/\/maintenance\/tasks\//);

  await page.getByRole("button", { name: "Avvia" }).click();
  await expect(page.getByRole("button", { name: "Avvia" })).toBeHidden();
  await page.getByRole("button", { name: "Registra esito" }).click();
  const outcome = page.getByRole("dialog");
  await outcome.getByLabel("Note").fill("Catena in ordine");
  await outcome.getByRole("button", { name: "Registra esito" }).click();
  await expect(outcome).toBeHidden();

  await expect
    .poll(async () => {
      const r = await adminRequest<{
        items: { title: string; status: string }[];
      }>(page, "GET", `${marinaApi(h.marinaId)}/maintenance/tasks?limit=200`);
      return r.data.items.filter((t) => t.title === title).map((t) => t.status);
    })
    .toEqual(["done"]);
});
