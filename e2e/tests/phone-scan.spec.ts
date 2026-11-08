import { expect, test } from "@playwright/test";

import { archiveOpsHarbour } from "../support/occupancy";
import { loginOnPhone, seedDockDay, transitQrPng } from "../support/phone";

// Transit QR (m6b-plan §6.4, §10.2): the desk shows the QR of a transit; the dock worker's phone reads it (here
// from a photo, the scanner's file fallback) and the transit card opens with Arrivato.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test("scan the transit QR, open the card, record the arrival", async ({
  page,
}) => {
  test.setTimeout(3 * 60_000);
  await loginOnPhone(page);
  const day = await seedDockDay(page);
  harbour = day.marinaId;

  // The desk transit page offers the QR for the dock worker.
  await page.goto(`/admin/m/${day.marinaId}/transits/${day.transit.id}`);
  await page.getByTestId("transit-qr-button").click();
  await expect(page.getByTestId("transit-qr-image")).toBeVisible();
  await page.keyboard.press("Escape");

  const png = await transitQrPng(page, day.marinaId, day.transit.id);
  await page.goto(`/admin/m/${day.marinaId}/dock?tab=scan`);
  await expect(page.getByTestId("camera-scanner")).toBeVisible();
  await page
    .getByTestId("camera-scanner-file")
    .setInputFiles({ name: "qr.png", mimeType: "image/png", buffer: png });
  const card = page.getByTestId("dock-scan-card");
  await expect(card).toBeVisible();
  await expect(card).toHaveAttribute("data-contract", day.transit.id);
  await expect(card).toContainText(day.transit.vessel);
  await expect(card).toContainText(day.berths[1]!.code);
  await card.getByRole("button", { name: "Arrivato" }).click();
  const sheet = page.getByTestId("desk-presence-dialog");
  await sheet.getByRole("button", { name: "Registra l'arrivo" }).click();
  await expect(sheet).toBeHidden();
  await page.getByRole("tab", { name: "Oggi" }).click();
  await expect(page.getByTestId("dock-page")).toContainText("Arrivate: 1");

  // A photo without a transit QR is refused.
  await page.getByRole("tab", { name: "Scansiona" }).click();
  const other = await page.request
    .get("/admin/favicon.svg")
    .then((r) => r.body())
    .catch(() => Buffer.alloc(0));
  if (other.length > 0) {
    await page
      .getByTestId("camera-scanner-file")
      .setInputFiles({
        name: "x.svg",
        mimeType: "image/svg+xml",
        buffer: other,
      });
    await expect(page.getByRole("alert")).toBeVisible();
  }
});
