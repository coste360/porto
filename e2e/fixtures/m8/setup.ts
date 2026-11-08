import path from "node:path";

import type { Page } from "@playwright/test";

import {
  adminRequest,
  getMap,
  marinaApi,
  type MapDoc,
} from "../../support/map";
import { setupOpsHarbour, setupPrices } from "../../support/occupancy";

// M8 online booking setup through the admin API (m8-plan §14.5), outside the timed part of the specs: a fresh
// harbour with online-sellable berths, the transit price list, the booking texts, the online settings, the Stripe
// driver in test mode against the local stub and the marina switched on with its allowed embed origins.

/** Webhook secret of the Stripe stub (e2e/support/stripe-stub.mjs). */
export const STUB_WEBHOOK_SECRET =
  process.env.PORTO_E2E_STRIPE_WEBHOOK_SECRET ?? "whsec_porto_e2e_stub";

export const INSURANCE_PHOTO = path.join(
  import.meta.dirname,
  "insurance-photo.png",
);

type Json = Record<string, unknown>;

export type OnlineMarina = {
  marinaId: string;
  slug: string;
  berthIds: string[];
};

async function publishTexts(page: Page, marinaId: string): Promise<void> {
  for (const [kind, it, en] of [
    [
      "terms",
      "Condizioni di prenotazione del Marina E2E.",
      "Booking terms of Marina E2E.",
    ],
    [
      "privacy",
      "Informativa privacy del Marina E2E.",
      "Privacy notice of Marina E2E.",
    ],
  ] as const) {
    await adminRequest(
      page,
      "POST",
      `${marinaApi(marinaId)}/online-booking/texts`,
      {
        body: { kind, texts: { it, en } },
      },
    );
  }
}

async function makeBerthsSellable(
  page: Page,
  marinaId: string,
  mapId: string,
): Promise<void> {
  const doc = await getMap(page, marinaId, mapId);
  const {
    id: _id,
    version,
    updated_at: _u,
    ...rest
  } = doc as MapDoc & { updated_at: string };
  await adminRequest(page, "PUT", `${marinaApi(marinaId)}/maps/${mapId}`, {
    ifMatch: version,
    body: {
      ...rest,
      berths: doc.berths.map((b) => ({ ...b, online_sellable: true })),
    },
  });
}

async function saveOnlineSettings(
  page: Page,
  marinaId: string,
  patch: Json,
): Promise<void> {
  const cur = await adminRequest<Json & { version: number }>(
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
        ...patch,
        rules: { ...(input.rules as Json), min_advance_hours: 0 },
      },
    },
  );
}

async function enableStripe(page: Page, marinaId: string): Promise<void> {
  const cur = await adminRequest<{ version?: number }>(
    page,
    "GET",
    `${marinaApi(marinaId)}/payment-providers/stripe/settings`,
  );
  const version = Number(
    (cur.etag ?? "").replace(/\D/g, "") || cur.data.version || 0,
  );
  await adminRequest(
    page,
    "PUT",
    `${marinaApi(marinaId)}/payment-providers/stripe/settings`,
    {
      ifMatch: version,
      body: {
        enabled: true,
        mode: "test",
        secret_key: "sk_test_porto_e2e",
        webhook_secret: STUB_WEBHOOK_SECRET,
        sepa_enabled: false,
        methods: ["card"],
      },
    },
  );
}

/** Switches booking on for a marina with the given allowed embed origins. */
export async function setBooking(
  page: Page,
  marinaId: string,
  enabled: boolean,
  origins: string[],
): Promise<void> {
  const m = await adminRequest<{ version: number }>(
    page,
    "GET",
    marinaApi(marinaId),
  );
  await adminRequest(page, "PATCH", marinaApi(marinaId), {
    ifMatch: m.data.version,
    body: { booking_enabled: enabled, allowed_embed_origins: origins },
  });
}

export async function setupOnlineMarina(
  page: Page,
  opts: { origins?: string[]; berths?: number; stripe?: boolean } = {},
): Promise<OnlineMarina> {
  const harbour = await setupOpsHarbour(page, {
    berths: opts.berths ?? 4,
    linearAreaM: 20,
  });
  const marinaId = harbour.marinaId;
  await setupPrices(page, marinaId);
  await makeBerthsSellable(page, marinaId, harbour.mapId);
  await publishTexts(page, marinaId);
  await saveOnlineSettings(page, marinaId, {
    payment_mode: "full",
    arrival_info: { vhf_channel: "9", phone: "+39 010 0000000" },
  });
  if (opts.stripe !== false) await enableStripe(page, marinaId);
  await setBooking(page, marinaId, true, opts.origins ?? []);
  const m = await adminRequest<{ slug: string }>(
    page,
    "GET",
    marinaApi(marinaId),
  );
  return {
    marinaId,
    slug: m.data.slug,
    berthIds: harbour.berths.map((b) => b.id),
  };
}
