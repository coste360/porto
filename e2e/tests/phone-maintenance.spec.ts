import { expect, test } from "@playwright/test";

import { adminRequest, marinaApi } from "../support/map";
import { archiveOpsHarbour, setupOpsHarbour } from "../support/occupancy";
import {
  setOffline,
  unsentOps,
  waitForDrain,
  waitForOfflineCache,
  waitForServiceWorker,
} from "../support/offline";
import { TINY_PNG, loginOnPhone } from "../support/phone";

// Maintenance report from the phone offline (m9-plan §8.4, §10, INV-71): a report with a photo saved with no
// network waits in the queue and is sent once on reconnection, as one task with its photo.

let harbour: string | null = null;
test.afterEach(async ({ page, context }) => {
  await setOffline(context, false);
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test("phone maintenance: offline report with photo replayed once", async ({
  page,
  context,
}) => {
  test.setTimeout(2 * 60_000);
  await loginOnPhone(page);
  const h = await setupOpsHarbour(page, { berths: 2 });
  harbour = h.marinaId;
  const title = `Bitta allentata ${Date.now()}`;

  await page.goto(`/admin/m/${h.marinaId}/maintenance-phone`);
  await expect(page.getByTestId("maintenance-phone")).toBeVisible();
  await waitForOfflineCache(page);
  await waitForServiceWorker(page);
  await page.getByRole("button", { name: "Segnala" }).click();
  const form = page.getByTestId("report-form");
  await expect(form.locator("#rep-el-map")).toContainText("Porto");

  await setOffline(context, true);
  await expect(page.getByTestId("offline-indicator")).toContainText("Offline");
  await form.locator("#rep-title").fill(title);
  await form.locator("#rep-photo").setInputFiles({
    name: "bitta.png",
    mimeType: "image/png",
    buffer: TINY_PNG,
  });
  await form.getByRole("button", { name: "Invia segnalazione" }).click();
  await expect
    .poll(async () => (await unsentOps(page)).length)
    .toBeGreaterThan(0);
  expect(
    (await unsentOps(page)).every((o) => o.kind === "maintenance.report"),
  ).toBe(true);

  await setOffline(context, false);
  await waitForDrain(page);
  const tasks = await adminRequest<{
    items: { title: string; files?: unknown[] }[];
  }>(page, "GET", `${marinaApi(h.marinaId)}/maintenance/tasks?limit=200`);
  expect(tasks.data.items.filter((t) => t.title === title)).toHaveLength(1);
});
