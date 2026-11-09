import { deflateSync } from "node:zlib";

import { expect, type Page } from "@playwright/test";

import { openCashSession } from "../support/billing";
import { adminRequest, marinaApi } from "../support/map";
import { marinaDay } from "../support/occupancy";
import { uniq } from "../support/registry";

// Rental world of the M11 phone specs (HANDOVER), built through the admin API in the shared marina: rental items
// priced by a list valid this year and next (half day morning 150.00, fuel 2.10, late 60.00/h), a 40 kW rib with a
// 500.00 deposit (licence required), the half-day-morning formula with metered fuel, the rental settings (deposit
// cash, card POS and pre-authorisation; checklist minimum photos 2 and 1), an open cash session, and a confirmed
// booking of today's morning band (in the day view of the phone).

export type RentalWorld = { marinaId: string; bookingId: string; number: string; unitCode: string };

/**
 * A 1×1 grey PNG photo whose pixel is the shade: the pixel itself differs, so the phone's JPEG re-encoding keeps
 * the uploads distinct (the server stores identical bytes of the same handover once).
 */
export function photoPng(shade: number): Buffer {
  const chunk = (type: string, data: Buffer) => {
    const body = Buffer.concat([Buffer.from(type, "ascii"), data]);
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const crc = Buffer.alloc(4);
    crc.writeUInt32BE(crc32(body));
    return Buffer.concat([len, body, crc]);
  };
  const ihdr = Buffer.from([0, 0, 0, 1, 0, 0, 0, 1, 8, 0, 0, 0, 0]); // 1×1, 8-bit greyscale
  const raw = Buffer.from([0, (shade * 37) & 0xff]); // filter byte + pixel
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", ihdr),
    chunk("IDAT", deflateSync(raw)),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

function crc32(buf: Buffer): number {
  let c = 0xffffffff;
  for (const b of buf) {
    c ^= b;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  }
  return (c ^ 0xffffffff) >>> 0;
}

export async function setupRental(page: Page, marinaId: string, opts: { customer?: boolean } = {}): Promise<RentalWorld> {
  const api = marinaApi(marinaId);
  const tag = uniq();
  const classes = await adminRequest<{ items: { id: string; code: string }[] }>(page, "GET", `${api}/revenue-classes`);
  const rental = (classes.data.items.find((c) => c.code === "rental") ?? classes.data.items[0]!).id;
  const items: Record<string, string> = {};
  for (const [code, unit] of [
    ["GOM", "hour"],
    ["FUEL", "piece"],
    ["LATE", "hour"],
    ["DANNI", "lump"],
  ] as const) {
    const it = await adminRequest<{ id: string }>(page, "POST", `${api}/items`, {
      body: { code: `${code}-${tag}`, name: { it: `${code} ${tag}` }, item_type: "service", unit, revenue_class_id: rental, price_includes_vat: true },
    });
    items[code] = it.data.id;
  }
  const year = Number(marinaDay().slice(0, 4));
  const range = { valid_from: `${year}-01-01`, valid_to: `${year + 1}-12-31` };
  const list = await adminRequest<{ id: string; version: number; settings: unknown }>(page, "POST", `${api}/price-lists`, {
    body: { code: `NOL-${tag}`, name: { it: "Noleggio" }, ...range, contract_types: ["charter"] },
  });
  const rule = (key: string, item: string, unit: string, cents: number, scope?: unknown) => ({
    key, stage: "base", name: key, item_id: item, basis: "flat", unit, amount_cents: cents, ...(scope ? { scope } : {}),
  });
  const saved = await adminRequest<{ document: { version: number } }>(page, "PUT", `${api}/price-lists/${list.data.id}`, {
    ifMatch: list.data.version,
    body: {
      code: `NOL-${tag}`, name: { it: "Noleggio" }, ...range, contract_types: [], settings: list.data.settings, periods: [], bands: [],
      rules: [
        rule("h", items.GOM!, "hour", 4500),
        rule("m", items.GOM!, "half_day", 15000, { time_bands: ["morning"] }),
        rule("f", items.FUEL!, "stay", 210),
        rule("l", items.LATE!, "hour", 6000),
        rule("d", items.DANNI!, "stay", 100),
      ],
    },
  });
  await adminRequest(page, "POST", `${api}/price-lists/${list.data.id}/activate`, { ifMatch: saved.data.document.version, body: {} });
  const set = await adminRequest<{ version: number; bands: unknown; checklist: unknown }>(page, "GET", `${api}/rental/settings`);
  await adminRequest(page, "PUT", `${api}/rental/settings`, {
    ifMatch: set.data.version,
    body: {
      open_from_slot: 16, open_to_slot: 38, bands: set.data.bands, buffer_slots: 1, desk_hold_minutes: 60, no_show_after_minutes: 600,
      licence_threshold_kw_milli: 30000, checklist: set.data.checklist, checkout_min_photos: 2, checkin_min_photos: 1,
      deposit_modes: ["cash", "card_pos", "preauth"], identity_retention_days: 365, damage_item_id: items.DANNI,
    },
  });
  const vessel = (
    await adminRequest<{ id: string }>(page, "POST", `${api}/vessels`, {
      body: { name: `Gommone ${tag}`, loa_cm: 580, beam_cm: 240, hull_type: "rib", engines: [{ power_kw: 40 }] },
    })
  ).data.id;
  const unitCode = `G${tag}`.slice(0, 12).toUpperCase();
  const unit = await adminRequest<{ id: string }>(page, "POST", `${api}/rental/units`, {
    body: { vessel_id: vessel, code: unitCode, category: "rib", seats: 8, fuel_type: "petrol", tank_milli: 60000, deposit_cents: 50000, item_id: items.GOM },
  });
  const formula = await adminRequest<{ id: string }>(page, "POST", `${api}/rental/formulas`, {
    body: {
      code: `MM-${tag}`.slice(0, 24), name: { it: "Mezza mattina" }, kind: "half_day", band_code: "morning", fuel: "metered",
      fuel_item_id: items.FUEL, late_item_id: items.LATE, grace_minutes: 15, late_step_minutes: 30,
    },
  });
  await openCashSession(page.request, marinaId, 0);
  const day = marinaDay();
  const body: Record<string, unknown> = {
    unit_id: unit.data.id, formula_id: formula.data.id, start: { date: day, slot: 16 }, end: { date: day, slot: 26 },
  };
  if (opts.customer) {
    const c = await adminRequest<{ customer: { id: string } }>(page, "POST", `${api}/customers`, {
      body: { kind: "person", first_name: "Mario", last_name: `Verdi ${tag}`, emails: [{ label: "home", value: `verdi-${tag}@example.test` }] },
    });
    body.customer_id = c.data.customer.id;
  }
  else body.renter = { name: `Ada ${tag}` };
  const b = await adminRequest<{ id: string; number: string; status: string }>(page, "POST", `${api}/rental/bookings`, { body });
  expect(b.data.status).toBe("confirmed");
  return { marinaId, bookingId: b.data.id, number: b.data.number, unitCode };
}

async function rawPost(page: Page, path: string, body: Buffer, contentType: string): Promise<void> {
  const csrf = (await page.context().cookies()).find((c) => c.name === "porto_csrf")?.value ?? "";
  const res = await page.request.post(path, {
    data: body,
    headers: { "Content-Type": contentType, "X-CSRF-Token": csrf, "Idempotency-Key": crypto.randomUUID() },
  });
  expect(res.ok(), `${path}: ${res.status()} ${await res.text()}`).toBeTruthy();
}

/** Completes the check-out of a world's booking through the API (identity, photos, screen signature, cash deposit,
 * checklist, check-out), so that the check-in spec starts with the boat at sea. */
export async function checkOutViaApi(page: Page, w: RentalWorld): Promise<void> {
  const B = `${marinaApi(w.marinaId)}/rental/bookings/${w.bookingId}`;
  const year = Number(marinaDay().slice(0, 4));
  await adminRequest(page, "PUT", `${B}/identity`, {
    body: {
      document_type: "identity_card", document_number: "CA12345XY", document_expires_on: `${year + 3}-01-01`,
      licence_number: "LT-998877", licence_expires_on: `${year + 3}-01-01`,
    },
  });
  let shade = 10;
  for (const kind of ["id_document", "licence", "boat", "boat"]) {
    await rawPost(page, `${B}/handovers/checkout/photos?kind=${kind}`, photoPng(shade++), "image/png");
  }
  await adminRequest(page, "POST", `${B}/contract/sign`, { body: { driver: "screen", signature_png: photoPng(99).toString("base64") } });
  await adminRequest(page, "POST", `${B}/deposit`, { body: { mode: "cash" } });
  const set = await adminRequest<{ checklist: { key: string }[] }>(page, "GET", `${marinaApi(w.marinaId)}/rental/settings`);
  await adminRequest(page, "PUT", `${B}/handovers/checkout/checklist`, {
    body: { client_ref: `co-${w.bookingId.slice(-12)}`, items: set.data.checklist.map((c) => ({ key: c.key, ok: true })), fuel_eighths: 8, engine_hours_milli: 120500 },
  });
  const b = await adminRequest<{ version: number }>(page, "GET", B);
  await adminRequest(page, "POST", `${B}/check-out`, { ifMatch: b.data.version, body: {} });
}
