import { expect, test, type Page, type Route } from "@playwright/test";

import { marinaIdOf } from "../support/api";
import { dayFromToday } from "../support/registry";
import { login } from "../support/session";

// Yard planner (m10-plan §6, §14.3): a tray task dragged with the pointer onto an employee's day opens the hours
// dialog with the qualification and capacity feedback; the batch sent to putAssignments carries the task version
// and the confirmed overrides; the keyboard alternative (arrows + Enter) does the same; the auto-plan proposal is
// computed without writing and confirmed as a batch. The planner endpoints are mocked: the backend rules are
// covered by the Go tests.

type Call = { method: string; path: string; body: unknown };

function mondayOf(iso: string): string {
  const d = new Date(`${iso}T00:00:00Z`);
  const wd = d.getUTCDay() === 0 ? 7 : d.getUTCDay();
  d.setUTCDate(d.getUTCDate() + 1 - wd);
  return d.toISOString().slice(0, 10);
}

function plus(iso: string, n: number): string {
  return new Date(Date.parse(`${iso}T00:00:00Z`) + n * 86_400_000).toISOString().slice(0, 10);
}

async function mockPlanner(page: Page, marinaId: string): Promise<Call[]> {
  const calls: Call[] = [];
  const monday = mondayOf(dayFromToday(0));
  const days = Array.from({ length: 7 }, (_, i) => plus(monday, i));
  const planner = {
    from: days[0],
    to: days[6],
    today: dayFromToday(0),
    rows: [
      {
        employee_id: "e1",
        code: "P1",
        display_name: "Paolo Neri",
        department: "Verniciatura",
        qualification_ids: ["q-paint"],
        days: days.map((date, i) => ({
          date,
          capacity_milli: i < 5 ? 8000 : 0,
          assigned_milli: i === 1 ? 6000 : 0,
          actual_milli: 0,
          load_bp: i < 5 ? (i === 1 ? 7500 : 0) : null,
          overload: false,
        })),
      },
      {
        employee_id: "e2",
        code: "M1",
        display_name: "Mario Gru",
        department: "Piazzale",
        qualification_ids: ["q-mech"],
        days: days.map((date, i) => ({
          date,
          capacity_milli: i < 5 ? 8000 : 0,
          assigned_milli: 0,
          actual_milli: 0,
          load_bp: i < 5 ? 0 : null,
          overload: false,
        })),
      },
    ],
    assignments: [],
    tasks: [
      {
        task_id: "t-sand",
        work_order_id: "o1",
        work_order_number: "C2026-000001",
        vessel_name: "Libeccio",
        title: "Carteggiatura",
        qualification_id: "q-paint",
        estimated_hours_milli: 6000,
        remaining_milli: 6000,
        assigned_milli: 0,
        progress_bp: 0,
        depends_on: [],
        version: 2,
      },
    ],
  };
  await page.route(`**/api/admin/v1/marinas/${marinaId}/yard/**`, async (route: Route) => {
    const req = route.request();
    const url = new URL(req.url());
    const path = url.pathname.replace(`/api/admin/v1/marinas/${marinaId}`, "");
    const body = req.postData() ? JSON.parse(req.postData()!) : undefined;
    calls.push({ method: req.method(), path, body });
    const json = (status: number, data: unknown) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(data) });
    if (path === "/yard/planner") return json(200, planner);
    if (path === "/yard/qualifications")
      return json(200, {
        items: [
          { id: "q-paint", code: "painter", name: { it: "Verniciatore" }, labour_item_id: "i1", default_cost_cents: 2600, lift_operator: false, active: true, sort_order: 0, version: 1, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" },
          { id: "q-mech", code: "lift", name: { it: "Gruista" }, labour_item_id: "i2", default_cost_cents: 2400, lift_operator: true, active: true, sort_order: 1, version: 1, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" },
        ],
      });
    if (path === "/yard/planner/assignments" && req.method() === "PUT")
      return json(200, { assignments: [], tasks: [{ task_id: "t-sand", version: 3 }] });
    if (path === "/yard/planner/proposal")
      return json(200, {
        from: days[2],
        to: plus(days[2]!, 13),
        changes: [
          { task_id: "t-sand", employee_id: "e1", work_date: days[2], hours_milli: 6000, task_version: 2 },
        ],
        unplaced: [],
      });
    return route.continue();
  });
  return calls;
}

test("yard planner: drag with overrides, keyboard alternative and auto-plan", async ({ page }) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  const calls = await mockPlanner(page, marinaId);
  const monday = mondayOf(dayFromToday(0));
  await page.goto(`/admin/m/${marinaId}/yard/planner`);
  const task = page.getByTestId("tray-task");
  await expect(task).toContainText("Carteggiatura");

  // Pointer drag onto M1 on Tuesday: M1 lacks the painter qualification.
  const target = page.getByTestId(`planner-cell-M1-${plus(monday, 1)}`);
  const from = (await task.boundingBox())!;
  const to = (await target.boundingBox())!;
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 8 });
  await page.mouse.up();
  const dialog = page.getByTestId("assign-dialog");
  await expect(dialog).toContainText("Carteggiatura a M1");
  const assign = dialog.getByRole("button", { name: "Assegna" });
  await expect(assign).toBeDisabled();
  await dialog.getByLabel(/non ha la qualifica/).check();
  await dialog.getByLabel("Ore").fill("9");
  await dialog.getByLabel(/Supera la capacità/).check();
  await assign.click();
  await expect(dialog).toBeHidden();
  let put = calls.filter((c) => c.method === "PUT").at(-1)!;
  expect(put.body).toEqual({
    changes: [{ task_id: "t-sand", employee_id: "e2", work_date: plus(monday, 1), hours_milli: 9000, task_version: 2 }],
    force_qualification: true,
    allow_overload: true,
  });

  // Keyboard: pick the task, arrows to P1 on Wednesday, Enter.
  await task.click();
  const grid = page.getByTestId("planner-grid");
  await grid.focus();
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("Enter");
  await expect(dialog).toContainText("Carteggiatura a P1");
  await dialog.getByRole("button", { name: "Assegna" }).click();
  await expect(dialog).toBeHidden();
  put = calls.filter((c) => c.method === "PUT").at(-1)!;
  expect(put.body).toMatchObject({
    changes: [{ employee_id: "e1", work_date: plus(monday, 2), hours_milli: 6000 }],
    force_qualification: false,
    allow_overload: false,
  });

  // Auto-plan: computed (no write), then confirmed as one batch.
  const before = calls.filter((c) => c.method === "PUT").length;
  await page.getByRole("button", { name: "Proposta automatica" }).click();
  const proposal = page.getByTestId("proposal-dialog");
  await proposal.getByRole("button", { name: "Calcola" }).click();
  await expect(proposal.getByTestId("proposal-changes")).toContainText("P1");
  expect(calls.filter((c) => c.method === "PUT").length).toBe(before);
  await proposal.getByRole("button", { name: "Conferma la proposta" }).click();
  await expect(proposal).toBeHidden();
  expect(calls.filter((c) => c.method === "PUT").length).toBe(before + 1);
});
