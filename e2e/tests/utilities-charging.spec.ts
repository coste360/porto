import { expect, test } from "@playwright/test";

import { adminRequest, marinaApi } from "../support/map";
import { archiveOpsHarbour, setupOpsHarbour } from "../support/occupancy";
import { login } from "../support/session";

// Utilities (m9-plan §9, §12.2): a meter with two readings shows on the meters page, its consumption on the
// consumptions page; the charging run answers with its summary (an unassigned consumption is never charged).

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test("utilities: meter, readings, consumptions and charging run", async ({
  page,
}) => {
  test.setTimeout(2 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 2 });
  harbour = h.marinaId;
  const api = marinaApi(h.marinaId);
  const code = `E2E-${Date.now() % 100000}`;
  const meter = await adminRequest<{ id: string }>(
    page,
    "POST",
    `${api}/utility-meters`,
    {
      body: { code, kind: "power", unit: "kwh" },
      expectStatus: 201,
    },
  );
  for (const [i, value] of ["100", "125,5"].entries()) {
    await adminRequest(page, "POST", `${api}/meter-readings`, {
      body: { meter_id: meter.data.id, value, client_ref: `e2e-${code}-${i}` },
      expectStatus: 201,
    });
  }

  await page.goto(`/admin/m/${h.marinaId}/utilities`);
  await expect(
    page.getByTestId("meter-row").filter({ hasText: code }),
  ).toBeVisible();

  // A meter serving no place: its consumption stays unassigned (to the marina) until assigned or excluded.
  await page.goto(`/admin/m/${h.marinaId}/utilities/consumptions`);
  await page.locator("#consumption-status").click();
  await page.getByRole("option", { name: "Non assegnato" }).click();
  await expect(page.getByTestId("consumption-row").first()).toBeVisible();
  await page.getByRole("button", { name: "Addebita consumi" }).click();
  await expect(page.getByTestId("utility-run-summary")).toBeVisible();
});
