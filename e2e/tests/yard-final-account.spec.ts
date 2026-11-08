import { expect, test } from "@playwright/test";

import { adminRequest, marinaApi } from "../support/map";
import { archiveOpsHarbour, setupOpsHarbour } from "../support/occupancy";
import { login } from "../support/session";
import { createAntifoulingQuote, setupYardCatalogue } from "../support/yard";

// Final account (m10-plan §9.4, acceptance criterion 2) in the panel: the order of the antifouling quote with the
// hours of §9.4 (approved in batch on the approval page), 6 tins issued and 1 returned, completed; the account tab
// shows revenue 1.900,90 without the external cost (1.720,90), the billing preview the remainder after the advance
// and the final billing brings the charged total to the account gross.

let marina: string | null = null;
test.afterEach(async ({ page }) => {
  if (marina) await archiveOpsHarbour(page, marina);
  marina = null;
});

test("yard final account: hours approved, materials, account and final billing", async ({ page }) => {
  test.setTimeout(3 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 2 });
  marina = h.marinaId;
  const api = marinaApi(h.marinaId);
  const w = await setupYardCatalogue(page, h.marinaId);
  const q = await createAntifoulingQuote(page, h.marinaId, w);
  await adminRequest(page, "POST", `${api}/yard/quotes/${q.id}/accept`, { ifMatch: q.version, body: {} });
  const orders = await adminRequest<{ items: { id: string; quote_id?: string }[] }>(page, "GET", `${api}/yard/work-orders`);
  const orderId = orders.data.items.find((o) => o.quote_id === q.id)!.id;
  const tasks = await adminRequest<{ items: { id: string; title: string }[] }>(page, "GET", `${api}/yard/work-orders/${orderId}/tasks`);
  const task = (title: string) => tasks.data.items.find((t) => t.title === title)!.id;
  const quals = await adminRequest<{ items: { id: string; code: string }[] }>(page, "GET", `${api}/yard/qualifications`);
  const qual = (code: string) => quals.data.items.find((x) => x.code === code)!.id;
  const suffix = Date.now().toString(36).slice(-4);
  const emp = async (code: string, cost: number, q: string) =>
    (
      await adminRequest<{ id: string }>(page, "POST", `${api}/yard/employees`, {
        body: { code: `${code}${suffix}`.slice(0, 16), first_name: code, last_name: "Test", hourly_cost_cents: cost, qualification_ids: [q] },
      })
    ).data.id;
  const luca = await emp("L", 2200, qual("hand"));
  const marco = await emp("M", 2400, qual("hand"));
  const giulia = await emp("G", 2600, qual("painter"));
  for (const [e, t, hours] of [
    [luca, task("Carteggiatura"), 5000],
    [marco, task("Carteggiatura"), 4000],
    [giulia, task("Antivegetativa"), 5500],
  ] as const) {
    await adminRequest(page, "POST", `${api}/timesheets`, {
      body: { client_ref: `e2e-${e}`, employee_id: e, task_id: t, hours_milli: hours },
    });
  }

  // Approval page: select all, approve.
  await page.goto(`/admin/m/${h.marinaId}/yard/timesheets`);
  await page.getByRole("checkbox", { name: "Seleziona tutte" }).click();
  await page.getByRole("button", { name: /Approva selezionate \(3\)/ }).click();
  await expect(page.getByText("Nessuna ora da approvare.")).toBeVisible();

  // Materials from the order page.
  await page.goto(`/admin/m/${h.marinaId}/yard/orders/${orderId}`);
  await page.getByRole("tab", { name: "Materiali" }).click();
  await page.getByLabel("Articolo").click();
  await page.getByRole("option", { name: /MAT-AVEG/ }).click();
  await page.getByLabel("Quantità").fill("6");
  await page.getByRole("button", { name: "Preleva" }).click();
  await expect(page.getByRole("cell", { name: "Prelievo" })).toBeVisible();
  await page.getByLabel("Quantità").fill("1");
  await page.getByRole("button", { name: "Rendi" }).click();
  await expect(page.getByRole("cell", { name: "Reso" })).toBeVisible();

  // Done and completed through the API; the account and the billing in the panel.
  for (const t of tasks.data.items) {
    await adminRequest(page, "POST", `${api}/yard/tasks/${t.id}/progress`, { body: { client_ref: `done-${t.id.slice(0, 8)}`, done: true } });
  }
  const o = await adminRequest<{ version: number }>(page, "GET", `${api}/yard/work-orders/${orderId}`);
  await adminRequest(page, "POST", `${api}/yard/work-orders/${orderId}/transition`, { ifMatch: o.data.version, body: { action: "complete" } });
  await page.reload();
  await page.getByRole("tab", { name: "Consuntivo" }).click();
  const account = page.getByTestId("final-account");
  await expect(account).toContainText("1.720,90");
  await expect(account).toContainText("623,15");
  await page.getByRole("tab", { name: "Addebiti" }).click();
  await page.getByRole("button", { name: "Anteprima" }).click();
  await expect(page.getByTestId("billing-preview")).toContainText("1.476,35");
  await page.getByRole("button", { name: "Crea addebito" }).click();
  await expect(page.getByText("Saldo")).toBeVisible();
  await expect
    .poll(async () => (await adminRequest<{ charged_gross_cents: number }>(page, "GET", `${api}/yard/work-orders/${orderId}/billings`)).data.charged_gross_cents)
    .toBe(209950);
});
