import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";

import { expect, type APIRequestContext } from "@playwright/test";

import { MAIL_DIR, OWNER } from "./env";
import { fakeFiscalCode } from "./registry";

// M5 billing helpers for the Playwright specs (m5-plan §9.5, §11.5; signatures fixed by CONTRACT, bodies by WEB).
// Every helper goes through the admin API of the real binary as the logged-in staff member (page.request shares
// the browser cookies), so the specs never touch the database. Nothing here talks to a payment provider: the
// whole suite runs offline with the manual driver.

export type MoneyCents = number;

type Json = Record<string, unknown>;

const api = (marinaId: string) => `/api/admin/v1/marinas/${marinaId}`;

async function csrf(request: APIRequestContext): Promise<string> {
  const state = await request.storageState();
  return state.cookies.find((c) => c.name === "porto_csrf")?.value ?? "";
}

/** Calls the admin API with the session cookies (CSRF, Idempotency-Key on POST, optional If-Match). */
export async function billingRequest<T>(
  request: APIRequestContext,
  method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE",
  url: string,
  opts: { body?: unknown; ifMatch?: number } = {},
): Promise<T> {
  const headers: Record<string, string> = { "X-CSRF-Token": await csrf(request) };
  if (method === "POST") headers["Idempotency-Key"] = crypto.randomUUID();
  if (opts.ifMatch !== undefined) headers["If-Match"] = `"${opts.ifMatch}"`;
  const res = await request.fetch(url, {
    method,
    headers,
    ...(opts.body !== undefined ? { data: opts.body } : {}),
  });
  expect(
    res.ok(),
    `${method} ${url}: ${res.status()} ${await res.text()}`,
  ).toBeTruthy();
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

const uniq = () => Date.now().toString(36).slice(-5).toUpperCase();

/** A service item at 22 % VAT, price including VAT (charges carry an explicit unit price in the specs). */
export async function createBillingItem(
  request: APIRequestContext,
  marinaId: string,
  code = `SRV-${uniq()}`,
): Promise<{ id: string; code: string }> {
  const classes = await billingRequest<{ items: { id: string }[] }>(
    request,
    "GET",
    `${api(marinaId)}/revenue-classes`,
  );
  const item = await billingRequest<{ id: string }>(
    request,
    "POST",
    `${api(marinaId)}/items`,
    {
      body: {
        code,
        name: { it: `Servizio ${code}`, en: `Service ${code}` },
        item_type: "service",
        unit: "piece",
        vat_rate_bp: 2200,
        price_includes_vat: true,
        revenue_class_id: classes.items[0]!.id,
      },
    },
  );
  return { id: item.id, code };
}

/** A person customer with an email and a fiscal code (complete fiscal data for the export). */
export async function createBillingCustomer(
  request: APIRequestContext,
  marinaId: string,
  email = `cliente-m5-${uniq().toLowerCase()}@e2e.porto.test`,
): Promise<{ id: string; display_name: string; email: string }> {
  const lastName = `Mcinque${uniq()}`;
  const data = await billingRequest<{
    customer: { id: string; display_name: string };
  }>(request, "POST", `${api(marinaId)}/customers`, {
    body: {
      kind: "person",
      first_name: "Cliente",
      last_name: lastName,
      fiscal_code: fakeFiscalCode(),
      emails: [{ label: "principale", value: email }],
    },
  });
  return { ...data.customer, email };
}

/** Bank details shown on the payment requests and the public pay page (manual driver). */
export async function setBankDetails(
  request: APIRequestContext,
  marinaId: string,
): Promise<void> {
  const current = await billingRequest<{ version: number }>(
    request,
    "GET",
    `${api(marinaId)}/contract-settings`,
  );
  await billingRequest(request, "PUT", `${api(marinaId)}/contract-settings`, {
    ifMatch: current.version,
    body: {
      bank_details: {
        iban: "IT60X0542811101000000123456",
        holder: OWNER.marina,
        bank: "Banca E2E",
      },
    },
  });
}

/** Creates a manual charge for a customer with one line of the given catalogue item; returns its id. */
export async function createManualCharge(
  request: APIRequestContext,
  marinaId: string,
  customerId: string,
  itemId: string,
  qtyMilli = 1000,
  unitCents: MoneyCents = 12_200,
  dueOn?: string,
): Promise<string> {
  const charge = await billingRequest<{ id: string }>(
    request,
    "POST",
    `${api(marinaId)}/charges`,
    {
      body: {
        origin_type: "manual",
        customer_id: customerId,
        lines: [{ item_id: itemId, qty_milli: qtyMilli, unit_cents: unitCents }],
        ...(dueOn ? { due_on: dueOn } : {}),
      },
    },
  );
  return charge.id;
}

/** Records a manual payment matched to charges (oldest first when chargeIds is empty); returns its id. */
export async function recordPayment(
  request: APIRequestContext,
  marinaId: string,
  customerId: string,
  amountCents: MoneyCents,
  method: "cash" | "card_pos" | "bank_transfer" | "cheque" | "other",
  chargeIds: string[] = [],
): Promise<string> {
  let matches: { charge_id: string; amount_cents: number }[] | undefined;
  if (chargeIds.length) {
    // Spread the amount over the given charges in order, never above a residual.
    let left = amountCents;
    matches = [];
    for (const id of chargeIds) {
      const c = await billingRequest<{ residual_cents: number }>(
        request,
        "GET",
        `${api(marinaId)}/charges/${id}`,
      );
      const take = Math.min(left, c.residual_cents);
      if (take > 0) matches.push({ charge_id: id, amount_cents: take });
      left -= take;
    }
  }
  const payment = await billingRequest<{ id: string }>(
    request,
    "POST",
    `${api(marinaId)}/payments`,
    {
      body: {
        customer_id: customerId,
        kind: "payment",
        method,
        amount_cents: amountCents,
        ...(matches ? { matches } : { auto_match: "oldest_due_first" }),
      },
    },
  );
  return payment.id;
}

/** Opens the cash session of the default register with a float; returns its id. */
export async function openCashSession(
  request: APIRequestContext,
  marinaId: string,
  floatCents: MoneyCents,
): Promise<string> {
  const current = await request.get(`${api(marinaId)}/cash-sessions/current`);
  if (current.ok()) return ((await current.json()) as { id: string }).id;
  const session = await billingRequest<{ id: string }>(
    request,
    "POST",
    `${api(marinaId)}/cash-sessions`,
    { body: { float_cents: floatCents } },
  );
  return session.id;
}

/** Exports the to_invoice charges of a period; returns the export id. */
export async function exportCharges(
  request: APIRequestContext,
  marinaId: string,
  from: string,
  to: string,
): Promise<string> {
  const exp = await billingRequest<{ id: string }>(
    request,
    "POST",
    `${api(marinaId)}/charge-exports`,
    { body: { period_from: from, period_to: to } },
  );
  return exp.id;
}

function decodeQuotedPrintable(body: string): string {
  const bytes = body
    .replace(/=\r?\n/g, "")
    .replace(/=([0-9A-F]{2})/gi, (_, hex: string) =>
      String.fromCharCode(parseInt(hex, 16)),
    );
  return Buffer.from(bytes, "latin1").toString("utf8");
}

/** Decoded text of every .eml whose headers mention `to`, oldest first. */
export function billingMessagesTo(to: string): string[] {
  return readdirSync(MAIL_DIR)
    .filter((f) => f.endsWith(".eml"))
    .sort()
    .map((f) => readFileSync(path.join(MAIL_DIR, f), "utf8"))
    .filter((raw) => {
      const headerEnd = raw.search(/\r?\n\r?\n/);
      return raw.slice(0, headerEnd).toLowerCase().includes(to.toLowerCase());
    })
    .map(decodeQuotedPrintable);
}

/** The pay links (`/b/{slug}/pay/{token}`) of a message, in order. */
export function payLinksIn(text: string): string[] {
  const re = /https?:\/\/[^\s"'<>]+\/b\/[a-z0-9-]+\/pay\/[A-Za-z0-9_-]+/g;
  return (text.match(re) ?? []).map((l) => l.replace(/&amp;/g, "&"));
}

/** Reads the payment link of the last payment request email sent to an address (dev mail directory). */
export async function payLinkFromMail(
  to: string,
  timeoutMs = 10_000,
): Promise<string> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const messages = billingMessagesTo(to);
    for (const text of [...messages].reverse()) {
      const links = payLinksIn(text);
      if (links.length) return links[links.length - 1]!;
    }
    if (Date.now() > deadline)
      throw new Error(`payLinkFromMail(${to}): nothing found in ${MAIL_DIR}`);
    await new Promise((r) => setTimeout(r, 250));
  }
}

/** Waits until `to` has received at least `count` messages; returns them. */
export async function waitForMessages(
  to: string,
  count: number,
  timeoutMs = 15_000,
): Promise<string[]> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const all = billingMessagesTo(to);
    if (all.length >= count) return all;
    if (Date.now() > deadline)
      throw new Error(`waitForMessages(${to}): ${all.length} < ${count}`);
    await new Promise((r) => setTimeout(r, 250));
  }
}

/** Today in the marina timezone (Europe/Rome) as YYYY-MM-DD, shifted by `days`. */
export function billingDay(days = 0): string {
  const d = new Date(Date.now() + days * 86_400_000);
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: "Europe/Rome",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(d);
}

export type { Json };

/** Picks a customer in a CustomerField (server-side search by last name) inside `scope`. */
export async function pickCustomer(
  page: import("@playwright/test").Page,
  scope: import("@playwright/test").Locator | import("@playwright/test").Page,
  label: string,
  displayName: string,
): Promise<void> {
  await scope.getByRole("combobox", { name: label }).click();
  await page.getByPlaceholder("Cerca per nome").fill(displayName.split(" ").pop()!);
  await page.getByRole("option", { name: displayName }).click();
}
