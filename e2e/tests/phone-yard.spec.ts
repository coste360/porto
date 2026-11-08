import { expect, test, type Page, type Route } from "@playwright/test";

import { marinaIdOf } from "../support/api";
import { setOffline, unsentOps, waitForDrain } from "../support/offline";
import { loginOnPhone } from "../support/phone";
import { dayFromToday } from "../support/registry";

// Yard phone (m10-plan §7, §14.3, D25, INV-71/83): the timer started and stopped with no network waits in the
// queue as two yard.timesheet ops; on reconnection the start is sent once, the stop once and bound to the id the
// start returned (no duplicates). The time entry and work list endpoints of ORDERS are mocked: their dedupe is
// covered by the Go acceptance test TestAcceptance_M10_Offline.

type Call = { method: string; path: string; queuedAt: string | null };

async function mockYard(page: Page, marinaId: string): Promise<Call[]> {
  const calls: Call[] = [];
  const today = dayFromToday(0);
  const entry = (status: string) => ({
    id: "ts-1",
    employee_id: "e1",
    work_order_id: "o1",
    task_id: "t1",
    work_date: today,
    hours_milli: status === "running" ? 0 : 250,
    source: "timer",
    status,
    note: "",
    late: false,
    version: 1,
    created_at: `${today}T08:00:00Z`,
    updated_at: `${today}T08:00:00Z`,
  });
  await page.route(`**/api/admin/v1/marinas/${marinaId}/**`, async (route: Route) => {
    const req = route.request();
    const path = new URL(req.url()).pathname.replace(`/api/admin/v1/marinas/${marinaId}`, "");
    const json = (status: number, data: unknown) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(data) });
    if (path === "/yard/my-work") {
      return json(200, {
        employee_id: "e1",
        today,
        assignments: [
          {
            assignment_id: "a1",
            task_id: "t1",
            task_title: "Carteggiatura",
            work_order_id: "o1",
            work_order_number: "C2026-000001",
            vessel_name: "Libeccio",
            work_date: today,
            hours_milli: 4000,
            task_status: "todo",
            progress_bp: 0,
            checklist: [{ key: "k1", label: "Carteggiare", done: false }],
            task_version: 1,
          },
        ],
      });
    }
    if (path === "/timesheets/timer" || path.startsWith("/timesheets/")) {
      calls.push({ method: req.method(), path, queuedAt: req.headers()["porto-offline-queued-at"] ?? null });
      return path === "/timesheets/timer" ? json(201, entry("running")) : json(200, entry("submitted"));
    }
    return route.continue();
  });
  return calls;
}

test("phone yard: timer offline then reconnect, no duplicates", async ({ page, context }) => {
  test.setTimeout(2 * 60_000);
  await loginOnPhone(page);
  const marinaId = marinaIdOf(page);
  const calls = await mockYard(page, marinaId);
  await page.goto(`/admin/m/${marinaId}/yard-phone`);
  await expect(page.getByTestId("yard-phone")).toBeVisible();
  await page.getByTestId("yard-assignment").click();
  const task = page.getByTestId("yard-task");
  await expect(task).toContainText("Carteggiatura");

  await setOffline(context, true);
  await task.getByRole("button", { name: "Avvia" }).click();
  await expect(task.getByRole("button", { name: "Ferma" })).toBeVisible();
  await task.getByRole("button", { name: "Ferma" }).click();
  await expect(task.getByRole("button", { name: "Avvia" })).toBeVisible();
  await expect.poll(async () => (await unsentOps(page)).length).toBe(2);
  expect((await unsentOps(page)).every((o) => o.kind === "yard.timesheet")).toBe(true);
  expect(calls).toHaveLength(0);

  await setOffline(context, false);
  await waitForDrain(page);
  expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual([
    "POST /timesheets/timer",
    "POST /timesheets/ts-1/stop",
  ]);
  expect(calls.every((c) => !!c.queuedAt)).toBe(true);
  const width = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(width).toBeLessThanOrEqual(page.viewportSize()!.width);
});
