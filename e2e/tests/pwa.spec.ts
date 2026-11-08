import { expect, test } from "@playwright/test";

import { waitForServiceWorker } from "../support/offline";
import { loginOnPhone } from "../support/phone";

// The admin panel as an installable PWA (m6b-plan §3): a valid manifest, a service worker that controls the page
// and caches the shell only, and the update prompt when a new build is published.

test("manifest and service worker headers", async ({ request }) => {
  const res = await request.get("/admin/site.webmanifest");
  expect(res.ok()).toBeTruthy();
  const m = (await res.json()) as Record<string, unknown>;
  expect(m).toMatchObject({
    start_url: "/admin/",
    scope: "/admin/",
    display: "standalone",
    orientation: "any",
    categories: ["business"],
  });
  expect((m.icons as { sizes: string }[]).map((i) => i.sizes)).toEqual(
    expect.arrayContaining(["192x192", "512x512"]),
  );
  expect((m.shortcuts as { name: string }[]).map((s) => s.name)).toEqual([
    "Oggi",
    "Scansiona",
  ]);

  const sw = await request.get("/admin/sw.js");
  expect(sw.ok()).toBeTruthy();
  expect(sw.headers()["cache-control"]).toBe("no-cache");
  expect(sw.headers()["service-worker-allowed"]).toBe("/admin/");
  const body = await sw.text();
  expect(body).not.toContain("__PORTO_PRECACHE__");
  expect(body).toContain("/admin/index.html");
  expect((await request.get("/admin/.vite/manifest.json")).status()).toBe(404);
});

test("the service worker controls the page and never caches API data", async ({
  page,
}) => {
  await loginOnPhone(page);
  await waitForServiceWorker(page);
  const caches = await page.evaluate(async () => {
    const names = await self.caches.keys();
    const urls: string[] = [];
    for (const n of names) {
      for (const r of await (await self.caches.open(n)).keys())
        urls.push(new URL(r.url).pathname);
    }
    return { names, urls };
  });
  expect(caches.names.length).toBe(1);
  expect(caches.names[0]).toMatch(/^porto-shell-[0-9a-f]{16}$/);
  expect(caches.urls).toContain("/admin/index.html");
  expect(caches.urls.filter((u) => u.startsWith("/api/"))).toEqual([]);
});

test("a new build shows the update prompt, and Aggiorna switches to it", async ({
  page,
}) => {
  await loginOnPhone(page);
  await waitForServiceWorker(page);
  const before = await page.evaluate(
    () => navigator.serviceWorker.controller?.scriptURL,
  );
  // A deploy publishes a new worker: here the same registration gets a new script URL, which the browser
  // installs as a new worker that waits (Playwright cannot rewrite the worker script itself).
  await page.evaluate(async () => {
    await navigator.serviceWorker.register("/admin/sw.js?build=next", {
      scope: "/admin/",
    });
  });
  await expect(page.getByText("Nuova versione disponibile")).toBeVisible({
    timeout: 20_000,
  });
  await expect(page.locator("html")).toHaveAttribute(
    "data-porto-sw-update",
    "waiting",
  );
  // The waiting worker takes over and the page reloads once it controls it.
  const reloaded = page.waitForEvent("load");
  await page.getByRole("button", { name: "Aggiorna" }).click();
  await reloaded;
  await expect
    .poll(
      () =>
        page
          .evaluate(() => navigator.serviceWorker.controller?.scriptURL ?? "")
          .catch(() => ""),
      {
        timeout: 20_000,
      },
    )
    .toContain("build=next");
  expect(before).not.toContain("build=next");
});
