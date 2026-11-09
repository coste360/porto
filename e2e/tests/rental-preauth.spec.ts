import { expect, test } from "@playwright/test";

import { STUB_WEBHOOK_SECRET } from "../fixtures/m8/setup";
import { setupRental } from "../helpers/rental";
import { marinaIdOf } from "../support/api";
import { adminRequest, marinaApi } from "../support/map";
import { loginOnPhone } from "../support/phone";

// Rental deposit pre-authorisation (m11-plan §9.2, D15): with the Stripe stub enabled the phone's deposit step
// opens the provider page (manual capture); the stub's signed webhook authorises it and the phone shows the
// deposit authorised through the rental.changed SSE invalidation, without a reload.

test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

test("rental phone: deposit pre-authorised through the provider page", async ({ page, context }) => {
  test.setTimeout(2 * 60_000);
  await loginOnPhone(page);
  const marinaId = marinaIdOf(page);
  const cur = await adminRequest<{ version?: number }>(page, "GET", `${marinaApi(marinaId)}/payment-providers/stripe/settings`);
  const version = Number((cur.etag ?? "").replace(/\D/g, "") || cur.data.version || 0);
  await adminRequest(page, "PUT", `${marinaApi(marinaId)}/payment-providers/stripe/settings`, {
    ifMatch: version,
    body: { enabled: true, mode: "test", secret_key: "sk_test_porto_e2e", webhook_secret: STUB_WEBHOOK_SECRET, sepa_enabled: false, methods: ["card"] },
  });
  const w = await setupRental(page, marinaId, { customer: true });
  const B = `${marinaApi(marinaId)}/rental/bookings/${w.bookingId}`;
  const year = new Date().getFullYear();
  await adminRequest(page, "PUT", `${B}/identity`, {
    body: { document_type: "passport", document_number: "YA0000001", document_expires_on: `${year + 3}-01-01`, licence_number: "L1", licence_expires_on: `${year + 3}-01-01` },
  });
  await page.goto(`/admin/m/${marinaId}/rental-phone`);
  await page.getByTestId("rental-day-entry").filter({ hasText: w.unitCode }).first().click();
  const wizard = page.getByTestId("checkout-wizard");
  await wizard.getByText("Cauzione", { exact: true }).click();
  await expect(wizard.getByTestId("deposit-step")).toBeVisible();
  await wizard.getByTestId("deposit-mode-preauth").click();
  await wizard.getByTestId("deposit-take").click();
  const link = wizard.getByTestId("deposit-link");
  await expect(link).toBeVisible();
  const href = (await link.getAttribute("href")) ?? "";
  expect(href).toMatch(/^https?:\/\//);
  const pay = await context.newPage();
  await pay.goto(href);
  await pay.getByRole("button", { name: "Paga" }).click();
  await pay.close();
  await expect(wizard.getByTestId("deposit-step").getByText("Autorizzata")).toBeVisible({ timeout: 20_000 });
  const dep = await adminRequest<{ status: string }>(page, "GET", `${B}/deposit`);
  expect(dep.data.status).toBe("authorized");
});
