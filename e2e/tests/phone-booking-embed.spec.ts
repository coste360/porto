import AxeBuilder from "@axe-core/playwright";
import { expect, test, type FrameLocator, type Page } from "@playwright/test";

import {
  INSURANCE_PHOTO,
  setupOnlineMarina,
  type OnlineMarina,
} from "../fixtures/m8/setup";
import { BASE_URL, HOST_URL, OWNER } from "../support/env";
import { archiveOpsHarbour, marinaDay } from "../support/occupancy";
import { loginOnPhone } from "../support/phone";

// M8 acceptance (spec 32, m8-plan §14.5): a complete booking from a smartphone, inside the widget iframe of the
// marina's own website (another origin), in under 2 minutes: dates and boat, category, extras, hold, details,
// consents, document photo, payment on the provider's hosted page (local Stripe stub, signed webhook), and the
// confirmation. The admin setup runs before the timer.

test.use({ viewport: { width: 390, height: 844 } });
test.setTimeout(180_000);

let marina: OnlineMarina | null = null;

test.afterEach(async ({ page }) => {
  if (marina) await archiveOpsHarbour(page, marina.marinaId);
  marina = null;
});

/** Fails on serious or critical axe findings (WCAG 2.x A/AA) on the page and its frames. */
async function expectAxeClean(page: Page, step: string) {
  const result = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  const serious = result.violations.filter(
    (v) => v.impact === "serious" || v.impact === "critical",
  );
  expect(
    serious.map(
      (v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`,
    ),
    `axe on step ${step}`,
  ).toEqual([]);
}

async function expectNoHorizontalScroll(frame: FrameLocator, step: string) {
  const overflow = await frame
    .locator("html")
    .evaluate((el) => el.scrollWidth - el.clientWidth);
  expect(overflow, `horizontal scroll on step ${step}`).toBeLessThanOrEqual(0);
}

test("books a berth from a phone inside the embedded widget in under 2 minutes", async ({
  page,
}) => {
  await loginOnPhone(page);
  marina = await setupOnlineMarina(page, { origins: [HOST_URL] });
  const slug = marina.slug;

  const t0 = Date.now();
  await page.goto(`${HOST_URL}/host.html?slug=${slug}`);
  const w = page.frameLocator("iframe");

  // 1. Dates and boat.
  await w.getByLabel("Arrivo").fill(marinaDay(3));
  await w.getByLabel("Partenza").fill(marinaDay(5));
  await w.getByLabel("Lunghezza fuori tutto").fill("11,50");
  await w.getByLabel("Larghezza").fill("3,30");
  await w.getByRole("combobox", { name: "Tipo di scafo" }).click();
  await w.getByRole("option", { name: "Monoscafo a vela" }).click();
  // The select's portal and the aria-hidden it puts on the page go away after its close animation.
  await expect(w.getByRole("listbox")).toBeHidden();
  await expect(w.locator("#root[aria-hidden]")).toHaveCount(0);
  await expectNoHorizontalScroll(w, "search");
  await expectAxeClean(page, "search");
  await w.getByRole("button", { name: "Cerca" }).click();

  // 2. Category.
  const category = w
    .getByTestId("booking-category")
    .and(w.locator(":enabled"))
    .first();
  await expect(category).toBeVisible();
  await expectNoHorizontalScroll(w, "berth");
  await expectAxeClean(page, "berth");
  await category.click();

  // 3. Extras and summary, then hold.
  await expect(w.getByTestId("price-total")).toBeVisible();
  await expectNoHorizontalScroll(w, "extras");
  await w.getByTestId("booking-hold").click();

  // 4. Details, document photo, consents.
  await expect(w.getByTestId("hold-countdown")).toBeVisible();
  await w.getByLabel("Nome", { exact: true }).fill("Giulia");
  await w.getByLabel("Cognome").fill("Rossi");
  await w.getByLabel("Email").fill(`m8-embed-${Date.now()}@example.test`);
  await w.getByLabel("Telefono").fill("+39 333 0000000");
  await w.getByLabel("Nome della barca").fill("Libeccio");
  await w.getByLabel("Persone a bordo").fill("2");
  await w.locator('input[type="file"]').first().setInputFiles(INSURANCE_PHOTO);
  await expect(
    w.getByTestId("booking-document").getByText("Caricato"),
  ).toBeVisible();
  await w.getByLabel("Accetto le condizioni di prenotazione").check();
  await w.getByLabel("Ho letto l'informativa sulla privacy").check();
  await expectNoHorizontalScroll(w, "details");
  await expectAxeClean(page, "details");
  await w.getByTestId("booking-details-submit").click();

  // 5. Payment: "Paga" navigates the top window to the hosted checkout of the stub.
  await expect(w.getByTestId("booking-due-now")).toBeVisible();
  await expectNoHorizontalScroll(w, "payment");
  await w.getByTestId("booking-pay").click();
  await page.waitForURL(/127\.0\.0\.1:18082\/pay\//);
  await page.getByRole("button", { name: "Paga" }).click();

  // 6. Porto's return page: verified by the signed webhook, never by the browser return.
  await page.waitForURL(
    new RegExp(`^${BASE_URL.replace(/\./g, "\\.")}/b/${slug}/r/`),
  );
  await expect(page.getByText("Prenotazione confermata")).toBeVisible({
    timeout: 60_000,
  });
  const elapsed = Date.now() - t0;
  expect(elapsed, `booking took ${elapsed} ms`).toBeLessThan(120_000);
  const reference =
    (await page.getByTestId("booking-reference").textContent())?.trim() ?? "";
  expect(reference).toMatch(/^[0-9A-Z]{8}$/);
  expect(page.url()).not.toContain("#st=");
  await expectAxeClean(page, "return");

  // "Torna al sito" goes back to the host page (allowed origin); the widget resumes the booking of this tab and
  // tells the host page that it is booked.
  await page.getByTestId("return-back").click();
  await page.waitForURL(new RegExp(`^${HOST_URL}/host\\.html`));
  await expect(w.getByTestId("booking-outcome")).toHaveAttribute(
    "data-outcome",
    "confirmed",
  );
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          (
            window as unknown as {
              __portoEvents: { type: string; reference?: string }[];
            }
          ).__portoEvents,
      ),
    )
    .toContainEqual({ type: "booked", reference });
});

test("a marina that does not allow the host origin cannot be framed", async ({
  page,
}) => {
  // The bootstrap marina has no allowed embed origin: frame-ancestors 'self' and X-Frame-Options SAMEORIGIN.
  const res = await page.request.get(`/b/${OWNER.slug}`);
  const csp = res.headers()["content-security-policy"] ?? "";
  expect(csp).toContain("frame-ancestors 'self'");
  expect(csp).not.toContain(HOST_URL);
  expect(res.headers()["x-frame-options"]).toBe("SAMEORIGIN");
  const api = await page.request.get(`/api/public/v1/${OWNER.slug}`);
  expect(api.headers()["x-frame-options"]).toBe("DENY");

  // Wait for the framed page's own response and for the frame to finish loading (the browser refuses to render it),
  // instead of a fixed pause: then the booking app must not be there.
  const framed = page.waitForResponse(
    (r) => r.url().includes(`/b/${OWNER.slug}`) && r.frame() !== page.mainFrame(),
  );
  await page.goto(`${HOST_URL}/host.html?slug=${OWNER.slug}`);
  await expect(page.locator("iframe")).toHaveCount(1);
  const frame = (await framed).frame();
  await frame.waitForLoadState("load");
  await expect(page.frameLocator("iframe").locator("#root")).toHaveCount(0);
});
