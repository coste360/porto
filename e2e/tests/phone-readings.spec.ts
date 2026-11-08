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
import { loginOnPhone } from "../support/phone";

// Dock readings offline (m9-plan §10, spec 29.2, INV-71, INV-83): a reading saved with no network waits in the
// queue and is sent once on reconnection.

let harbour: string | null = null;
test.afterEach(async ({ page, context }) => {
  await setOffline(context, false);
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test("phone readings: offline reading replayed once", async ({
  page,
  context,
}) => {
  test.setTimeout(2 * 60_000);
  await loginOnPhone(page);
  const h = await setupOpsHarbour(page, { berths: 2 });
  harbour = h.marinaId;
  const api = marinaApi(h.marinaId);
  const code = `PH-${Date.now() % 100000}`;
  const meter = await adminRequest<{ id: string }>(
    page,
    "POST",
    `${api}/utility-meters`,
    {
      body: { code, kind: "water", unit: "m3" },
      expectStatus: 201,
    },
  );

  await page.goto(`/admin/m/${h.marinaId}/dock?tab=readings`);
  const tab = page.getByTestId("readings-tab");
  await expect(
    tab.getByTestId("dock-meter").filter({ hasText: code }),
  ).toBeVisible();
  await waitForOfflineCache(page);
  await waitForServiceWorker(page);
  await expect(
    tab.getByTestId("dock-meter").filter({ hasText: code }),
  ).toBeVisible();

  await setOffline(context, true);
  await expect(page.getByTestId("offline-indicator")).toContainText("Offline");
  await tab.getByTestId("dock-meter").filter({ hasText: code }).click();
  const form = page.getByTestId("reading-form");
  await form.locator("#dock-value").fill("42");
  await form.getByRole("button", { name: "Salva lettura" }).click();
  await expect
    .poll(async () => (await unsentOps(page)).map((o) => o.kind))
    .toEqual(["utility.reading"]);

  await setOffline(context, false);
  await waitForDrain(page);
  const readings = await adminRequest<{ items: { id: string }[] }>(
    page,
    "GET",
    `${api}/meter-readings?meter_id=${meter.data.id}`,
  );
  expect(readings.data.items).toHaveLength(1);
});
