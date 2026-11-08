import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";

import {
  expect,
  test,
  type APIRequestContext,
  type Page,
} from "@playwright/test";

import {
  INSURANCE_PHOTO,
  setupOnlineMarina,
  type OnlineMarina,
} from "../fixtures/m8/setup";
import { MAIL_DIR } from "../support/env";
import { adminRequest, marinaApi } from "../support/map";
import { archiveOpsHarbour, marinaDay } from "../support/occupancy";
import { login } from "../support/session";

// M8 approval flow (m8-plan §6, §14.5): a booking over the approval length threshold waits for the desk. With
// the Stripe stub the card is pre-authorised; the desk approves, the stub captures and its signed webhook
// confirms the transit. The reject path voids the authorisation and emails the customer.

let marina: OnlineMarina | null = null;

test.afterEach(async ({ page }) => {
  if (marina) await archiveOpsHarbour(page, marina.marinaId);
  marina = null;
});

type Created = {
  booking_id: string;
  booking_token: string;
  reference: string;
  approval_required: boolean;
};

async function requireApprovalOver(
  page: Page,
  marinaId: string,
  loaCm: number,
) {
  const cur = await adminRequest<Record<string, unknown> & { version: number }>(
    page,
    "GET",
    `${marinaApi(marinaId)}/online-booking/settings`,
  );
  const {
    version,
    booking_enabled: _b,
    embed_origins: _e,
    saved: _s,
    ...input
  } = cur.data;
  await adminRequest(
    page,
    "PUT",
    `${marinaApi(marinaId)}/online-booking/settings`,
    {
      ifMatch: version,
      body: {
        ...input,
        approval: {
          always: false,
          group_ids: [],
          hold_hours: 48,
          loa_over_cm: loaCm,
        },
      },
    },
  );
}

/** Books through the public API up to the hosted checkout; returns the booking and the checkout URL. */
async function bookOverThreshold(
  request: APIRequestContext,
  slug: string,
  email: string,
) {
  const P = `/api/public/v1/${slug}`;
  const arrival = marinaDay(3);
  const departure = marinaDay(5);
  const vessel = { loa_cm: 1150, beam_cm: 330, hull_type: "monohull_sail" };
  const avail = await request.get(`${P}/booking/availability`, {
    params: {
      arrival,
      departure,
      loa_cm: vessel.loa_cm,
      beam_cm: vessel.beam_cm,
    },
  });
  expect(avail.ok()).toBeTruthy();
  const cat = (
    (await avail.json()) as {
      categories: {
        code: string;
        available: boolean;
        approval_required: boolean;
      }[];
    }
  ).categories.find((c) => c.available);
  expect(cat?.approval_required).toBe(true);
  const created = await request.post(`${P}/bookings`, {
    headers: { "Idempotency-Key": crypto.randomUUID() },
    data: { arrival, departure, vessel, category: cat?.code, locale: "it" },
  });
  expect(created.status()).toBe(201);
  const b = (await created.json()) as Created;
  const auth = { "Porto-Booking-Token": b.booking_token };
  const config = (await (await request.get(`${P}/booking/config`)).json()) as {
    texts: { kind: string; version: number }[];
  };
  const version = (k: string) =>
    config.texts.find((x) => x.kind === k)?.version ?? 1;
  const details = await request.put(`${P}/bookings/${b.booking_id}/details`, {
    headers: auth,
    data: {
      customer: {
        first_name: "Anna",
        last_name: "Bianchi",
        email,
        phone: "+39 333 1111111",
        country: "IT",
        locale: "it",
      },
      vessel: { name: "Grecale", hull_type: "monohull_sail" },
      people_on_board: 2,
      consents: {
        terms_version: version("terms"),
        privacy_version: version("privacy"),
        marketing: false,
      },
    },
  });
  expect(details.ok(), await details.text()).toBeTruthy();
  const doc = await request.post(
    `${P}/bookings/${b.booking_id}/documents?type=insurance_policy`,
    {
      headers: {
        ...auth,
        "Content-Type": "image/png",
        "X-File-Name": "polizza.png",
      },
      data: readFileSync(INSURANCE_PHOTO),
    },
  );
  expect(doc.status()).toBe(201);
  const pay = await request.post(`${P}/bookings/${b.booking_id}/pay`, {
    headers: { ...auth, "Idempotency-Key": crypto.randomUUID() },
    data: {},
  });
  expect(pay.ok(), await pay.text()).toBeTruthy();
  const result = (await pay.json()) as {
    redirect_url?: string;
    status: string;
  };
  return { booking: b, auth, checkout: result.redirect_url ?? "" };
}

async function status(
  request: APIRequestContext,
  slug: string,
  id: string,
  auth: Record<string, string>,
) {
  const r = await request.get(`/api/public/v1/${slug}/bookings/${id}/status`, {
    headers: auth,
  });
  return ((await r.json()) as { status: string }).status;
}

function mailTo(email: string): string[] {
  return readdirSync(MAIL_DIR)
    .filter((f) => f.endsWith(".eml"))
    .map((f) => readFileSync(path.join(MAIL_DIR, f), "utf8"))
    .filter((raw) =>
      raw
        .slice(0, raw.search(/\r?\n\r?\n/))
        .toLowerCase()
        .includes(email.toLowerCase()),
    );
}

test("a booking over the length threshold is approved by the desk and confirmed by the capture", async ({
  page,
}) => {
  await login(page);
  marina = await setupOnlineMarina(page);
  await requireApprovalOver(page, marina.marinaId, 1000);
  const email = `m8-approve-${Date.now()}@example.test`;
  const { booking, auth, checkout } = await bookOverThreshold(
    page.request,
    marina.slug,
    email,
  );

  // The customer pays on the hosted page: the stub only authorises the card (manual capture).
  expect(checkout).toMatch(/127\.0\.0\.1:18082\/pay\//);
  await page.goto(checkout);
  await page.getByRole("button", { name: "Paga" }).click();
  await expect
    .poll(() => status(page.request, marina!.slug, booking.booking_id, auth), {
      timeout: 30_000,
    })
    .toBe("awaiting_approval");

  // The desk sees it with the approval reason and approves it.
  await page.goto(`/admin/m/${marina.marinaId}/online`);
  await page.getByRole("button", { name: booking.reference }).click();
  const sheet = page.getByTestId("online-booking-sheet");
  await expect(sheet.getByText("Lunghezza oltre la soglia")).toBeVisible();
  await sheet.getByTestId("online-approve").click();
  await expect
    .poll(() => status(page.request, marina!.slug, booking.booking_id, auth), {
      timeout: 60_000,
    })
    .toBe("confirmed");
});

test("a rejected booking releases the berth and the customer is told", async ({
  page,
}) => {
  await login(page);
  marina = await setupOnlineMarina(page);
  await requireApprovalOver(page, marina.marinaId, 1000);
  const email = `m8-reject-${Date.now()}@example.test`;
  const { booking, auth, checkout } = await bookOverThreshold(
    page.request,
    marina.slug,
    email,
  );
  await page.goto(checkout);
  await page.getByRole("button", { name: "Paga" }).click();
  await expect
    .poll(() => status(page.request, marina!.slug, booking.booking_id, auth), {
      timeout: 30_000,
    })
    .toBe("awaiting_approval");
  const before = mailTo(email).length;

  await page.goto(`/admin/m/${marina.marinaId}/online`);
  await page.getByRole("button", { name: booking.reference }).click();
  await page.getByTestId("online-reject").click();
  await page.getByLabel("Motivo").fill("Nessun posto adatto per quelle date");
  await page.getByTestId("online-reject-confirm").click();
  await expect
    .poll(() => status(page.request, marina!.slug, booking.booking_id, auth), {
      timeout: 30_000,
    })
    .toBe("cancelled");
  await expect
    .poll(() => mailTo(email).length, { timeout: 30_000 })
    .toBeGreaterThan(before);
});
