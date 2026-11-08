import { expect, test } from "@playwright/test";

import { adminRequest, marinaApi } from "../support/map";
import { archiveOpsHarbour, setupOpsHarbour } from "../support/occupancy";
import { login } from "../support/session";
import { createAntifoulingQuote, setupYardCatalogue } from "../support/yard";

// Boats in the yard (m10-plan §5, §14.2): the antifouling quote accepted from the panel becomes a work order with
// its two tasks; the board shows it among the planned orders; it is started from the action menu, a task is marked
// done and the order is put on hold and resumed; the server records each transition.

let marina: string | null = null;
test.afterEach(async ({ page }) => {
  if (marina) await archiveOpsHarbour(page, marina);
  marina = null;
});

test("yard orders: board, transitions and tasks of the accepted quote", async ({ page }) => {
  test.setTimeout(2 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 2 });
  marina = h.marinaId;
  const api = marinaApi(h.marinaId);
  const w = await setupYardCatalogue(page, h.marinaId);
  const q = await createAntifoulingQuote(page, h.marinaId, w);
  await adminRequest(page, "POST", `${api}/yard/quotes/${q.id}/accept`, { ifMatch: q.version, body: {} });
  const orders = await adminRequest<{ items: { id: string; number: string; quote_id?: string }[] }>(page, "GET", `${api}/yard/work-orders`);
  const order = orders.data.items.find((o) => o.quote_id === q.id)!;

  await page.goto(`/admin/m/${h.marinaId}/yard/orders`);
  const card = page.getByTestId(`board-card-${order.number}`);
  await expect(card).toBeVisible();
  await expect(card).toContainText(w.vesselName);
  await card.getByRole("button", { name: "Azioni" }).click();
  await page.getByRole("menuitem", { name: "Avvia" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Conferma" }).click();
  await expect(page.getByRole("dialog")).toBeHidden();

  await card.getByRole("link", { name: order.number }).click();
  await expect(page).toHaveURL(new RegExp(`/yard/orders/${order.id}$`));
  await expect(page.getByTestId("work-order-page")).toContainText("In lavorazione");
  await page.getByRole("tab", { name: "Attività" }).click();
  await expect(page.getByTestId("task-1")).toContainText("Carteggiatura");
  await page.getByTestId("task-1").getByRole("button", { name: "Segna fatta" }).click();
  await expect(page.getByTestId("task-1")).toContainText("Fatta");

  await page.getByRole("button", { name: "Azioni" }).click();
  await page.getByRole("menuitem", { name: "Sospendi" }).click();
  await page.getByRole("dialog").getByRole("textbox").fill("Attesa ricambi");
  await page.getByRole("dialog").getByRole("button", { name: "Conferma" }).click();
  await expect(page.getByRole("dialog")).toBeHidden();
  // The status is shown on the summary tab.
  await page.getByRole("tab", { name: "Riepilogo" }).click();
  await expect(page.getByTestId("work-order-page")).toContainText("Sospesa");
  await page.getByRole("button", { name: "Azioni" }).click();
  await page.getByRole("menuitem", { name: "Riprendi" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Conferma" }).click();

  await expect
    .poll(async () => (await adminRequest<{ status: string; completion_bp: number }>(page, "GET", `${api}/yard/work-orders/${order.id}`)).data)
    .toMatchObject({ status: "in_progress", completion_bp: 6000 });
});
