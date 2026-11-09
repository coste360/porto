import { expect, test } from "@playwright/test";

import { Human } from "../helpers/human";
import { checkOutViaApi, photoPng, setupRental } from "../helpers/rental";
import { marinaIdOf } from "../support/api";
import { loginOnPhone } from "../support/phone";

// Rental check-in on a phone (spec 32 M11 criterion 2, m11-plan §8, D25): the boat checked out through the API, then
// one scripted check-in without damage (deposit released; a late return is collected by card), timed end to end
// with accounted human time: under 3 minutes, ≤ 18 taps, ≤ 1 typed field, 1 photo.

test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

test("rental phone: check-in in under 3 minutes", async ({ page }, info) => {
  test.setTimeout(3 * 60_000);
  await loginOnPhone(page);
  const w = await setupRental(page, marinaIdOf(page));
  await checkOutViaApi(page, w);
  await page.goto(`/admin/m/${w.marinaId}/rental-phone`);
  await expect(page.getByTestId("rental-phone")).toBeVisible();

  const h = new Human(page);
  await h.screen();
  await h.tap(page.getByTestId("rental-day-entry").filter({ hasText: w.unitCode }).first());
  const wizard = page.getByTestId("checkin-wizard");
  await expect(wizard.getByTestId("return-step")).toBeVisible();
  await h.screen();
  await h.tap(wizard.getByTestId("return-next"));

  await expect(wizard.getByTestId("boat-step-checkin")).toBeVisible();
  await h.screen();
  await h.tap(wizard.getByTestId("checklist-all-ok"));
  await h.type(wizard.getByLabel("Ore motore"), "125");
  await h.photo(wizard.getByTestId("boat-photo-checkin"), photoPng(7));
  await expect(wizard.getByText("Foto della barca (1)")).toBeVisible();
  await h.tap(wizard.getByTestId("boat-save"));

  await expect(wizard.getByTestId("damages-step")).toBeVisible();
  await h.screen();
  await h.tap(wizard.getByTestId("damages-none"));
  await expect(wizard.getByTestId("extras-step")).toBeVisible();
  await h.screen();
  await h.tap(wizard.getByTestId("extras-next"));

  await expect(wizard.getByTestId("checkin-preview")).toBeVisible();
  await h.screen();
  const total = (await wizard.getByTestId("return-total").textContent()) ?? "";
  await expect(wizard.getByTestId("checkin-commit")).toBeEnabled();
  await h.tap(wizard.getByTestId("checkin-commit"));
  if (!/^\s*0,00/.test(total)) {
    await h.tap(wizard.getByTestId("checkin-cash"));
    await h.tap(page.getByTestId("quick-cash-dialog").getByRole("button", { name: "Carta (POS)" }));
  }
  await expect(page.getByTestId("rental-phone")).toBeVisible();

  await h.report(info, "check-in");
  expect(h.taps).toBeLessThanOrEqual(18);
  expect(h.fields).toBeLessThanOrEqual(1);
  expect(h.photos).toBeLessThanOrEqual(1);
  expect(h.totalMs()).toBeLessThan(180_000);
});
