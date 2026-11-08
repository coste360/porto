import { expect, test, type Page } from "@playwright/test";

import { OWNER } from "../support/env";
import { archiveOpsHarbour } from "../support/occupancy";
import { today } from "../support/opsmap";
import {
  cashPaymentsOn,
  databases,
  presencesOf,
  setOffline,
  storeCount,
  storedOps,
  unsentOps,
  waitForDrain,
  waitForOfflineCache,
  waitForServiceWorker,
} from "../support/offline";
import {
  TINY_PNG,
  loginOnPhone,
  seedDockDay,
  type DockDay,
} from "../support/phone";

// Offline queue on a phone (m6b-plan §10.1, spec 29.2, INV-71, INV-83, INV-84): arrivals, departures, a photo and
// a cash collection recorded offline are sent once on reconnection; online-only controls say why; a lost response
// is retried with the same key; a conflict surfaces to the user and is never resolved automatically; logout
// deletes the offline data.

let harbour: string | null = null;
test.afterEach(async ({ page, context }) => {
  await setOffline(context, false);
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

async function seed(page: Page): Promise<DockDay> {
  await loginOnPhone(page);
  const day = await seedDockDay(page);
  harbour = day.marinaId;
  return day;
}

/** Opens each dock tab the offline work needs, so that their queries are cached, then waits for the save. */
async function warmUp(page: Page, day: DockDay) {
  const base = `/admin/m/${day.marinaId}/dock`;
  await page.goto(`${base}?tab=cash`);
  await expect(page.getByTestId("dock-cash")).toBeVisible();
  await expect(page.getByTestId("dock-cash-session")).toContainText(
    "Cassa aperta dal",
  );
  await waitForOfflineCache(page);
  await page.goto(`${base}?tab=today`);
  await expect(
    page.getByTestId(`dock-row-${await vesselIdOf(page, day)}`),
  ).toBeVisible();
  await waitForOfflineCache(page);
}

async function vesselIdOf(page: Page, day: DockDay): Promise<string> {
  const res = await page.request.get(
    `/api/admin/v1/marinas/${day.marinaId}/contracts/${day.transit.id}`,
  );
  expect(res.ok()).toBeTruthy();
  return ((await res.json()) as { vessel: { id: string } }).vessel.id;
}

async function recordPresence(
  page: Page,
  vesselId: string,
  event: "Arrivato" | "Partito",
  note?: string,
) {
  const row = page.getByTestId(`dock-row-${vesselId}`).first();
  await row.getByRole("button", { name: event }).click();
  const sheet = page.getByTestId("desk-presence-dialog");
  if (note) await sheet.getByLabel("Note").fill(note);
  if (event === "Arrivato") {
    await sheet.locator("input[type=file]").setInputFiles({
      name: "banchina.png",
      mimeType: "image/png",
      buffer: TINY_PNG,
    });
  }
  await sheet
    .getByRole("button", {
      name: event === "Arrivato" ? "Registra l'arrivo" : "Registra la partenza",
    })
    .click();
  await expect(sheet).toBeHidden();
}

test("offline: arrival with note and photo, departure and cash are sent once on reconnection", async ({
  page,
  context,
}) => {
  test.setTimeout(4 * 60_000);
  const day = await seed(page);
  const vesselId = await vesselIdOf(page, day);
  await warmUp(page, day);
  await waitForServiceWorker(page);

  await setOffline(context, true);
  await expect(page.getByTestId("offline-indicator")).toContainText("Offline");

  await recordPresence(page, vesselId, "Arrivato", "Ormeggio di prua");
  await expect(
    page
      .getByTestId(`dock-row-${vesselId}`)
      .first()
      .getByTestId("dock-row-queued"),
  ).toBeVisible();
  await recordPresence(page, vesselId, "Partito");

  await page.getByRole("tab", { name: "Cassa" }).click();
  const collectible = page
    .locator('[data-testid^="dock-collectible-"]')
    .first();
  await collectible.getByRole("button", { name: "Incassa" }).click();
  await page
    .getByTestId("dock-collect-sheet")
    .getByRole("button", { name: "Registra l'incasso" })
    .click();
  await expect(
    collectible.getByTestId("dock-collectible-queued"),
  ).toBeVisible();

  // Online-only controls are disabled with the reason, inline.
  await page.getByRole("tab", { name: "Altro" }).click();
  await expect(
    page.getByTestId("dock-more").getByTestId("offline-reason").first(),
  ).toBeVisible();

  // Reload while offline: the shell comes from the service worker, the queue from IndexedDB.
  await page.reload();
  await expect(page.getByTestId("dock-page")).toBeVisible();
  const queued = await unsentOps(page);
  expect(queued.map((o) => o.kind).sort()).toEqual([
    "payment.cash",
    "presence.photo",
    "presence.record",
    "presence.record",
  ]);
  expect(new Set(queued.map((o) => o.key)).size).toBe(4);

  await setOffline(context, false);
  await waitForDrain(page);
  const presences = await presencesOf(page, day.marinaId, vesselId, today());
  expect(presences.map((p) => p.event).sort()).toEqual(["arrived", "departed"]);
  expect(presences.find((p) => p.event === "arrived")?.has_photo).toBe(true);
  expect(await cashPaymentsOn(page, day.marinaId, today())).toHaveLength(1);
});

test("a lost response is retried with the same key: still one presence", async ({
  page,
}) => {
  test.setTimeout(3 * 60_000);
  const day = await seed(page);
  const vesselId = await vesselIdOf(page, day);
  await warmUp(page, day);
  let dropped = 0;
  const keys: string[] = [];
  await page.route(
    `**/api/admin/v1/marinas/${day.marinaId}/presences`,
    async (route) => {
      if (route.request().method() !== "POST") return route.continue();
      keys.push(route.request().headers()["idempotency-key"] ?? "");
      if (dropped++ === 0) {
        await route.fetch(); // the server records it…
        return route.abort("connectionreset"); // …and the answer is lost
      }
      return route.continue();
    },
  );
  await recordPresence(page, vesselId, "Arrivato");
  await waitForDrain(page);
  expect(keys.length).toBeGreaterThanOrEqual(2);
  expect(new Set(keys).size).toBe(1);
  expect(await presencesOf(page, day.marinaId, vesselId, today())).toHaveLength(
    1,
  );
});

test("a conflict on replay is shown and never resolved silently; logout purges the offline data", async ({
  page,
  context,
}) => {
  test.setTimeout(3 * 60_000);
  const day = await seed(page);
  const vesselId = await vesselIdOf(page, day);
  await warmUp(page, day);

  await setOffline(context, true);
  await recordPresence(page, vesselId, "Arrivato");

  // Meanwhile the office moves the allocation to another berth (API requests are not affected by the emulation).
  const contract = (await (
    await page.request.get(
      `/api/admin/v1/marinas/${day.marinaId}/contracts/${day.transit.id}`,
    )
  ).json()) as {
    allocations: { id: string; status: string; version: number }[];
  };
  const alloc = contract.allocations.find((a) => a.status === "active")!;
  const csrf =
    (await context.cookies()).find((c) => c.name === "porto_csrf")?.value ?? "";
  const move = await page.request.post(
    `/api/admin/v1/marinas/${day.marinaId}/occupancies/move`,
    {
      data: {
        allocation_id: alloc.id,
        version: alloc.version,
        from_night: today(),
        target: { resource_type: "berth", resource_id: day.berths[4]!.id },
      },
      headers: { "X-CSRF-Token": csrf, "Idempotency-Key": crypto.randomUUID() },
    },
  );
  expect(move.ok(), `move: ${move.status()} ${await move.text()}`).toBeTruthy();

  await setOffline(context, false);
  await expect
    .poll(
      async () =>
        (await storedOps(page))
          .map((o) => [o.state, o.problem?.type])
          .sort((a, b) => String(a[0]).localeCompare(String(b[0]))),
      { timeout: 30_000 },
    )
    .toEqual([
      ["blocked", undefined],
      ["conflict", "allocation_changed"],
    ]);
  expect(await presencesOf(page, day.marinaId, vesselId, today())).toHaveLength(
    0,
  );

  // The user sees the conflict with the new berth and discards it.
  await page.getByTestId("offline-indicator").click();
  const sheet = page.getByTestId("queue-sheet");
  await expect(sheet).toContainText("L'ormeggio della barca è cambiato");
  // The conflicted arrival and its photo (blocked behind it) are discarded by the user, one by one.
  for (let i = 0; i < 2; i++) {
    await sheet.getByRole("button", { name: "Scarta" }).first().click();
    await expect.poll(async () => (await storedOps(page)).length).toBe(1 - i);
  }
  await page.keyboard.press("Escape");

  // Logout deletes the offline database.
  await page.goto(`/admin/m/${day.marinaId}/dock?tab=more`);
  await page.getByTestId("dock-logout").click();
  await expect(page).toHaveURL(/\/admin\/login/);
  await expect.poll(() => databases(page)).not.toContain("porto-offline");
  // Signed in again through the API, for the cleanup of the harbour.
  const relogin = await page.request.post("/api/admin/v1/auth/login", {
    data: { email: OWNER.email, password: OWNER.password },
  });
  expect(relogin.ok()).toBeTruthy();
});

test("an expired session or revoked device purges the cached marina data, even with an empty queue", async ({
  page,
  context,
}) => {
  test.setTimeout(2 * 60_000);
  const day = await seed(page);
  await warmUp(page, day);
  expect(await storeCount(page, "cache")).toBeGreaterThan(0);
  expect(await unsentOps(page)).toHaveLength(0);

  // The session dies on the server side (same answer as a revoked device): getMe answers 401.
  const cookies = await context.cookies();
  await context.clearCookies();
  await context.addCookies(cookies.filter((c) => c.name !== "porto_session"));
  await page.reload();
  await expect(page).toHaveURL(/\/admin\/login/);
  await expect
    .poll(() => storeCount(page, "cache"), { timeout: 10_000 })
    .toBe(0);
  await expect
    .poll(() => storeCount(page, "meta"), { timeout: 10_000 })
    .toBe(0);

  const relogin = await page.request.post("/api/admin/v1/auth/login", {
    data: { email: OWNER.email, password: OWNER.password },
  });
  expect(relogin.ok()).toBeTruthy();
});
