import { expect, type BrowserContext, type Page } from "@playwright/test";

// Helpers of the M6b offline specs (m6b-plan §10.1): the cache warm-up marker, the offline queue as stored in
// IndexedDB (porto-offline), the service worker, and the presences / payments seen by the server.

/** Waits until the offline cache was saved once (the API provider marks <html data-porto-offline-cache>). */
export async function waitForOfflineCache(page: Page): Promise<void> {
  await expect(page.locator("html")).toHaveAttribute(
    "data-porto-offline-cache",
    "saved",
    {
      timeout: 20_000,
    },
  );
}

/** Waits until a service worker controls the page (reloads once when the first install just finished). */
export async function waitForServiceWorker(page: Page): Promise<void> {
  await page.evaluate(async () => {
    await navigator.serviceWorker.ready;
  });
  if (!(await page.evaluate(() => !!navigator.serviceWorker.controller))) {
    await page.reload();
  }
  await expect
    .poll(() => page.evaluate(() => !!navigator.serviceWorker.controller))
    .toBe(true);
}

export type StoredOp = {
  id: string;
  kind: string;
  key: string;
  state: string;
  attempts: number;
  request: { operationId: string; path: string; body?: unknown };
  problem?: { status: number; type: string; detail?: unknown };
};

/** The ops of the queue as stored in IndexedDB. */
export async function storedOps(page: Page): Promise<StoredOp[]> {
  return page.evaluate(
    () =>
      new Promise<StoredOp[]>((resolve) => {
        const req = indexedDB.open("porto-offline");
        req.onerror = () => resolve([]);
        req.onsuccess = () => {
          const db = req.result;
          if (!db.objectStoreNames.contains("ops")) {
            db.close();
            resolve([]);
            return;
          }
          const all = db
            .transaction("ops", "readonly")
            .objectStore("ops")
            .getAll();
          all.onsuccess = () => {
            db.close();
            resolve(all.result as StoredOp[]);
          };
          all.onerror = () => {
            db.close();
            resolve([]);
          };
        };
      }),
  );
}

/** Number of records of an offline store ("cache", "meta", ...): 0 when the database or store is missing. */
export async function storeCount(page: Page, store: string): Promise<number> {
  return page.evaluate(
    (name) =>
      new Promise<number>((resolve) => {
        const req = indexedDB.open("porto-offline");
        req.onerror = () => resolve(0);
        req.onsuccess = () => {
          const db = req.result;
          if (!db.objectStoreNames.contains(name)) {
            db.close();
            resolve(0);
            return;
          }
          const n = db.transaction(name, "readonly").objectStore(name).count();
          n.onsuccess = () => {
            db.close();
            resolve(n.result);
          };
          n.onerror = () => {
            db.close();
            resolve(0);
          };
        };
      }),
    store,
  );
}

/** Ops not yet done (what the indicator counts as pending, plus conflicts). */
export async function unsentOps(page: Page): Promise<StoredOp[]> {
  return (await storedOps(page)).filter((o) => o.state !== "done");
}

/** Waits until every op is done (the queue drained). */
export async function waitForDrain(
  page: Page,
  timeout = 30_000,
): Promise<void> {
  await expect
    .poll(
      async () => (await unsentOps(page)).map((o) => `${o.kind}:${o.state}`),
      { timeout },
    )
    .toEqual([]);
}

/** The names of the IndexedDB databases of the origin. */
export async function databases(page: Page): Promise<string[]> {
  return page.evaluate(async () =>
    (await indexedDB.databases()).map((d) => d.name ?? ""),
  );
}

/** Offline for the browser (navigator.onLine false, every request fails) or back online. */
export async function setOffline(
  context: BrowserContext,
  offline: boolean,
): Promise<void> {
  await context.setOffline(offline);
}

type Page_<T> = { items: T[] };

/** Presences of a vessel recorded on the server (admin API, the page's session). */
export async function presencesOf(
  page: Page,
  marinaId: string,
  vesselId: string,
  date: string,
) {
  const res = await page.request.get(
    `/api/admin/v1/marinas/${marinaId}/presences?date=${date}&vessel_id=${vesselId}&limit=200`,
  );
  expect(res.ok(), `presences: ${res.status()}`).toBeTruthy();
  return (
    (await res.json()) as Page_<{
      id: string;
      event: string;
      has_photo?: boolean;
    }>
  ).items;
}

/** Payments recorded on the server for a cash session day (admin API). */
export async function cashPaymentsOn(
  page: Page,
  marinaId: string,
  date: string,
) {
  const res = await page.request.get(
    `/api/admin/v1/marinas/${marinaId}/payments?method=cash&from=${date}&to=${date}&limit=200`,
  );
  expect(res.ok(), `payments: ${res.status()}`).toBeTruthy();
  return ((await res.json()) as Page_<{ id: string; amount_cents: number }>)
    .items;
}
