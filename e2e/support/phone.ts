import { expect, type Page } from "@playwright/test";

import { OWNER } from "./env";
import {
  archiveLeftoverHarbours,
  createContractViaApi,
  createTransitViaApi,
  openHarbourCount,
  setupOpsHarbour,
  setupPrices,
} from "./occupancy";
import { openCashSession } from "./billing";
import { today } from "./opsmap";
import { createCustomer, createVessel, dayFromToday, uniq } from "./registry";
import { fillLogin } from "./session";

// Helpers of the M6b phone specs (m6b-plan §10.2): sign-in on a phone (the phone shell has no sidebar user menu),
// a day at the dock seeded through the admin API (synthetic data only), and the transit QR as a PNG photo for the
// scanner's file fallback.

/** A 1×1 PNG, the smallest valid presence photo. */
export const TINY_PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

/**
 * Signs in through the login form on a phone and waits for a marina screen (the phone shell or, for roles
 * without a phone screen, the app shell). A 429 of the login limiter is waited out, as in `login`.
 */
export async function loginOnPhone(
  page: Page,
  email: string = OWNER.email,
  password: string = OWNER.password,
): Promise<void> {
  await page.goto("/admin/login");
  for (let attempt = 0; ; attempt++) {
    const response = page.waitForResponse(
      (r) =>
        r.url().endsWith("/api/admin/v1/auth/login") &&
        r.request().method() === "POST",
    );
    await fillLogin(page, email, password);
    const res = await response;
    if (res.status() !== 429 || attempt >= 2) break;
    const wait =
      Math.min(Number(res.headers()["retry-after"] ?? "5") || 5, 60) + 1;
    await page.waitForTimeout(wait * 1000);
  }
  await expect(page).toHaveURL(/\/admin\/m\/[^/]+/);
  if (openHarbourCount() > 0) await archiveLeftoverHarbours(page);
}

export type DockDay = {
  marinaId: string;
  berths: { id: string; code: string }[];
  tag: string;
  /** Confirmed transit arriving today (vessel `Ostro <tag>`), with its charge to collect. */
  transit: { id: string; allocationId: string; vessel: string };
  /** Active annual contract (vessel `Zefiro <tag>`), for releases. */
  annual: { vessel: string };
};

/** A fresh marina with prices, an arriving transit, an annual contract and an open cash session. */
export async function seedDockDay(page: Page): Promise<DockDay> {
  const h = await setupOpsHarbour(page, { berths: 6 });
  await setupPrices(page, h.marinaId);
  // The helpers below read the marina from the page URL.
  await page.goto(`/admin/m/${h.marinaId}/dock`);
  const tag = uniq();
  const owner = await createCustomer(page, {
    kind: "person",
    first_name: "Nino",
    last_name: `Phone ${tag}`,
  });
  const annual = await createVessel(page, {
    name: `Zefiro ${tag}`,
    loa_cm: 1000,
    beam_cm: 350,
  });
  const visitor = await createVessel(page, {
    name: `Ostro ${tag}`,
    loa_cm: 900,
    beam_cm: 320,
  });
  await createContractViaApi(page, h.marinaId, {
    vesselId: annual.id,
    customerId: owner.id,
    berthId: h.berths[0]!.id,
    term: "annual",
    startsOn: today(),
    activate: true,
  });
  const transit = await createTransitViaApi(page, h.marinaId, {
    vesselId: visitor.id,
    berthId: h.berths[1]!.id,
    arrival: today(),
    departure: dayFromToday(2),
    confirm: true,
  });
  await openCashSession(page.request, h.marinaId, 10_000);
  return {
    marinaId: h.marinaId,
    berths: h.berths.map((b) => ({ id: b.id, code: b.code })),
    tag,
    transit: { ...transit, vessel: `Ostro ${tag}` },
    annual: { vessel: `Zefiro ${tag}` },
  };
}

/** The phone layout holds at the current viewport: no horizontal scroll, bottom tabs at least 44 px tall. */
export async function expectPhoneLayout(
  page: Page,
  screenTestId: string,
): Promise<void> {
  const width = page.viewportSize()!.width;
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(width);
  const tabs = page.getByTestId(screenTestId).getByRole("tab");
  for (const tab of await tabs.all())
    expect((await tab.boundingBox())!.height).toBeGreaterThanOrEqual(44);
}

/** The transit QR (getTransitQr, SVG) rendered to a PNG in the page, as a photo of a printed QR would be. */
export async function transitQrPng(
  page: Page,
  marinaId: string,
  contractId: string,
): Promise<Buffer> {
  const res = await page.request.get(
    `/api/admin/v1/marinas/${marinaId}/transits/${contractId}/qr`,
  );
  expect(res.ok(), `qr: ${res.status()}`).toBeTruthy();
  expect(res.headers()["content-type"]).toBe("image/svg+xml");
  const svg = await res.text();
  const b64 = await page.evaluate(async (text) => {
    const img = new Image();
    img.src = `data:image/svg+xml;base64,${btoa(text)}`;
    await img.decode();
    const canvas = document.createElement("canvas");
    canvas.width = 480;
    canvas.height = 480;
    const ctx = canvas.getContext("2d")!;
    ctx.fillStyle = "white";
    ctx.fillRect(0, 0, 480, 480);
    ctx.drawImage(img, 40, 40, 400, 400);
    return canvas.toDataURL("image/png").split(",")[1]!;
  }, svg);
  return Buffer.from(b64, "base64");
}
