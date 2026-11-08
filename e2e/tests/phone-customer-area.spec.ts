import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";

import { expect, test, type BrowserContext } from "@playwright/test";

import {
  INSURANCE_PHOTO,
  setupOnlineMarina,
  type OnlineMarina,
} from "../fixtures/m8/setup";
import { MAIL_DIR } from "../support/env";
import { archiveOpsHarbour, marinaDay } from "../support/occupancy";
import { loginOnPhone } from "../support/phone";

// M8 customer area on a phone (m8-plan §7, §14.5): login link from the mail directory (token in the fragment),
// bookings list, document upload, cancellation with the refund preview of the contract's policy snapshot.

test.use({ viewport: { width: 390, height: 844 } });
test.setTimeout(180_000);

let marina: OnlineMarina | null = null;
// The admin cookies, put back for the cleanup after the customer-area part cleared them.
let adminCookies: Awaited<ReturnType<BrowserContext["cookies"]>> = [];

test.afterEach(async ({ page }) => {
  if (adminCookies.length > 0) await page.context().addCookies(adminCookies);
  adminCookies = [];
  if (marina) await archiveOpsHarbour(page, marina.marinaId);
  marina = null;
});

function decodeQP(body: string): string {
  return Buffer.from(
    body
      .replace(/=\r?\n/g, "")
      .replace(/=([0-9A-F]{2})/gi, (_, h: string) =>
        String.fromCharCode(parseInt(h, 16)),
      ),
    "latin1",
  ).toString("utf8");
}

/** Waits for the newest customer-area login link sent to email. */
async function waitForLoginLink(email: string, slug: string): Promise<string> {
  const re = new RegExp(`https?://[^\\s"'<>]+/b/${slug}/a#t=[A-Za-z0-9]+`);
  const deadline = Date.now() + 30_000;
  for (;;) {
    const links = readdirSync(MAIL_DIR)
      .filter((f) => f.endsWith(".eml"))
      .sort()
      .map((f) => readFileSync(path.join(MAIL_DIR, f), "utf8"))
      .filter((raw) =>
        raw
          .slice(0, raw.search(/\r?\n\r?\n/))
          .toLowerCase()
          .includes(email),
      )
      .map((raw) => decodeQP(raw).match(re)?.[0])
      .filter((x): x is string => Boolean(x));
    const last = links[links.length - 1];
    if (last) return last;
    if (Date.now() > deadline) throw new Error(`no login link for ${email}`);
    await new Promise((r) => setTimeout(r, 250));
  }
}

test("signs in with the emailed link, uploads a document and cancels with the refund preview", async ({
  page,
}) => {
  await loginOnPhone(page);
  marina = await setupOnlineMarina(page);
  const slug = marina.slug;
  const email = `m8-area-${Date.now()}@example.test`;
  const P = `/api/public/v1/${slug}`;

  // A confirmed booking through the public API and the stub checkout (setup, not under test here).
  const arrival = marinaDay(20);
  const departure = marinaDay(22);
  const vessel = { loa_cm: 900, beam_cm: 320, hull_type: "monohull_sail" };
  const avail = (await (
    await page.request.get(`${P}/booking/availability`, {
      params: {
        arrival,
        departure,
        loa_cm: vessel.loa_cm,
        beam_cm: vessel.beam_cm,
      },
    })
  ).json()) as { categories: { code: string; available: boolean }[] };
  const category = avail.categories.find((c) => c.available)?.code;
  const created = (await (
    await page.request.post(`${P}/bookings`, {
      headers: { "Idempotency-Key": crypto.randomUUID() },
      data: { arrival, departure, vessel, category, locale: "it" },
    })
  ).json()) as { booking_id: string; booking_token: string; reference: string };
  const auth = { "Porto-Booking-Token": created.booking_token };
  const config = (await (
    await page.request.get(`${P}/booking/config`)
  ).json()) as {
    texts: { kind: string; version: number }[];
  };
  const v = (k: string) => config.texts.find((x) => x.kind === k)?.version ?? 1;
  expect(
    (
      await page.request.put(`${P}/bookings/${created.booking_id}/details`, {
        headers: auth,
        data: {
          customer: {
            first_name: "Marta",
            last_name: "Neri",
            email,
            phone: "+39 333 2222222",
            country: "IT",
            locale: "it",
          },
          vessel: { name: "Maestrale", hull_type: "monohull_sail" },
          people_on_board: 3,
          consents: {
            terms_version: v("terms"),
            privacy_version: v("privacy"),
            marketing: false,
          },
        },
      })
    ).ok(),
  ).toBeTruthy();
  await page.request.post(
    `${P}/bookings/${created.booking_id}/documents?type=insurance_policy`,
    {
      headers: {
        ...auth,
        "Content-Type": "image/png",
        "X-File-Name": "polizza.png",
      },
      data: readFileSync(INSURANCE_PHOTO),
    },
  );
  const pay = (await (
    await page.request.post(`${P}/bookings/${created.booking_id}/pay`, {
      headers: { ...auth, "Idempotency-Key": crypto.randomUUID() },
      data: {},
    })
  ).json()) as { redirect_url: string };
  await page.goto(pay.redirect_url);
  await page.getByRole("button", { name: "Paga" }).click();
  await expect(page.getByText("Prenotazione confermata")).toBeVisible({
    timeout: 60_000,
  });

  // Login: the answer never says whether the email is known.
  adminCookies = await page.context().cookies();
  await page.context().clearCookies();
  await page.goto(`/b/${slug}/account`);
  await page.getByLabel("Email").fill(email);
  await page.getByTestId("account-login-submit").click();
  await expect(page.getByTestId("account-link-sent")).toBeVisible();
  const link = await waitForLoginLink(email, slug);
  await page.goto(link);
  await expect(page.getByTestId("account-bookings")).toBeVisible();
  expect(page.url()).not.toContain("#t=");
  const overflow = await page
    .locator("html")
    .evaluate((el) => el.scrollWidth - el.clientWidth);
  expect(overflow).toBeLessThanOrEqual(0);

  // Booking detail: QR, upload of a second document.
  await page.getByTestId("account-open").first().click();
  await expect(page.getByTestId("account-booking")).toContainText(
    created.reference,
  );
  await expect(page.getByTestId("account-qr")).toContainText("PT1:");
  await page.getByTestId("account-doc-input").setInputFiles(INSURANCE_PHOTO);
  await expect(page.getByText("Documento caricato.")).toBeVisible();

  // Cancel: the refund preview comes from the policy snapshot; confirming cancels.
  await page.getByTestId("account-cancel").click();
  const dialog = page.getByTestId("account-cancel-dialog");
  await expect(dialog.getByTestId("account-refund")).toBeVisible();
  await dialog.getByTestId("account-cancel-confirm").click();
  await expect(dialog).toBeHidden();
  await expect(page.getByTestId("account-status")).toHaveText("Annullata");
});
