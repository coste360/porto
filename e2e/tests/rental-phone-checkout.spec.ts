import { expect, test } from "@playwright/test";

import { Human } from "../helpers/human";
import { photoPng, setupRental } from "../helpers/rental";
import { marinaIdOf } from "../support/api";
import { loginOnPhone } from "../support/phone";

// Rental check-out on a phone (spec 32 M11 criterion 2, m11-plan §7, D25): one scripted flow on a booking of today
// (licence required, cash deposit, two boat photos), timed end to end with accounted human time; the flow stays
// under 3 minutes and within the interaction caps (≤ 22 taps, ≤ 5 typed fields, 4 photos, 1 signature).

test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

test("rental phone: check-out in under 3 minutes", async ({ page }, info) => {
  test.setTimeout(3 * 60_000);
  await loginOnPhone(page);
  const w = await setupRental(page, marinaIdOf(page));
  await page.goto(`/admin/m/${w.marinaId}/rental-phone`);
  await expect(page.getByTestId("rental-phone")).toBeVisible();

  const h = new Human(page);
  await h.screen();
  await h.tap(page.getByTestId("rental-day-entry").filter({ hasText: w.unitCode }).first());
  const wizard = page.getByTestId("checkout-wizard");
  await expect(wizard.getByTestId("identity-step")).toBeVisible();
  await h.screen();
  await h.type(wizard.getByLabel("Numero del documento"), "CA12345XY");
  const year = new Date().getFullYear();
  await h.type(wizard.getByLabel("Scadenza del documento"), `${year + 3}-01-01`);
  await h.photo(wizard.getByTestId("photo-id-document"), photoPng(1));
  await h.tap(wizard.getByTestId("identity-save"));

  await expect(wizard.getByTestId("licence-step")).toBeVisible();
  await h.screen();
  await h.type(wizard.getByLabel("Numero della patente"), "LT-998877");
  await h.type(wizard.getByLabel("Scadenza della patente"), `${year + 3}-05-01`);
  await h.photo(wizard.getByTestId("photo-licence"), photoPng(2));
  await h.tap(wizard.getByTestId("identity-save"));

  await expect(wizard.getByTestId("contract-step")).toBeVisible();
  await h.screen();
  await h.sign(wizard.getByTestId("signature-pad"));
  await h.tap(wizard.getByTestId("contract-sign"));

  await expect(wizard.getByTestId("deposit-step")).toBeVisible();
  await h.screen();
  await h.tap(wizard.getByTestId("deposit-take"));

  await expect(wizard.getByTestId("boat-step-checkout")).toBeVisible();
  await h.screen();
  await h.tap(wizard.getByTestId("checklist-all-ok"));
  await h.type(wizard.getByLabel("Ore motore"), "120,5");
  await h.photo(wizard.getByTestId("boat-photo-checkout"), photoPng(3));
  await h.photo(wizard.getByTestId("boat-photo-checkout"), photoPng(4));
  await expect(wizard.getByText("Foto della barca (2)")).toBeVisible();
  await h.tap(wizard.getByTestId("boat-save"));

  await expect(wizard.getByTestId("checkout-summary")).toBeVisible();
  await h.screen();
  await expect(wizard.getByTestId("checkout-commit")).toBeEnabled();
  await h.tap(wizard.getByTestId("checkout-commit"));
  await h.tap(wizard.getByTestId("checkout-cash"));
  await h.tap(page.getByTestId("quick-cash-dialog").getByRole("button", { name: "Contanti" }));
  await expect(page.getByTestId("rental-phone")).toBeVisible();

  await h.report(info, "check-out");
  expect(h.taps).toBeLessThanOrEqual(22);
  expect(h.fields).toBeLessThanOrEqual(5);
  expect(h.photos).toBeLessThanOrEqual(4);
  expect(h.signatures).toBe(1);
  expect(h.totalMs()).toBeLessThan(180_000);
});
