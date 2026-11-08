import { expect, test } from "@playwright/test";

import { mockStorage } from "../fixtures/m9/storage";
import { marinaIdOf } from "../support/api";
import { login } from "../support/session";

// Storage phone screen (m9-plan §12.1): at 360 px the movements of today are listed in order with the equipment
// and the blocked exits; "Fatto" marks one done (online).

test("storage phone: today's movements and done", async ({ page }) => {
  await login(page);
  await page.setViewportSize({ width: 360, height: 760 });
  const marinaId = marinaIdOf(page);
  const calls = await mockStorage(page, marinaId);
  await page.goto(`/admin/m/${marinaId}/storage-phone`);
  await expect(page.getByTestId("storage-phone")).toBeVisible();
  await expect(page.getByTestId("storage-movement-warnings")).toBeVisible();
  const mv = page.locator('[data-movement="mv-1"]');
  await expect(mv).toContainText("Bora");
  await expect(mv).toContainText("Movimentatore invasature");
  await mv.getByRole("button", { name: "Fatto" }).click();
  await expect
    .poll(() => calls.some((c) => c.path === "/storage/movements/mv-1/done"))
    .toBe(true);
  const width = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(width).toBeLessThanOrEqual(360);
});
