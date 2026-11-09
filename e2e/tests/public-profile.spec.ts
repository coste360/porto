import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

import { setupOnlineMarina, type OnlineMarina } from "../fixtures/m8/setup";
import { adminRequest, marinaApi } from "../support/map";
import { archiveOpsHarbour } from "../support/occupancy";
import { login } from "../support/session";

// M12 public marina profile /p/{slug} (m12-plan §12.3, §17.4): an anonymous visitor sees the published profile
// with its server-injected JSON-LD, switches it/en, follows "Prenota" to /b/{slug}; no response sets cookies and
// the page carries no berth code.

type Profile = { version: number; published: boolean };

async function publishProfile(page: Page, marinaId: string): Promise<void> {
  const m = await adminRequest<{ version: number }>(
    page,
    "GET",
    marinaApi(marinaId),
  );
  await adminRequest(page, "PATCH", marinaApi(marinaId), {
    ifMatch: m.data.version,
    body: { locales: ["it", "en"] },
  });
  const cur = await adminRequest<Profile>(
    page,
    "GET",
    `${marinaApi(marinaId)}/profile`,
  );
  const saved = await adminRequest<Profile>(
    page,
    "PUT",
    `${marinaApi(marinaId)}/profile`,
    {
      ifMatch: cur.data.version,
      body: {
        locales: ["it", "en"],
        entrance_lat_e7: 437101234,
        entrance_lon_e7: 104012345,
        phone: "+39 050 1112222",
        email: "ufficio@marina-e2e.example",
        vhf_channels: [9],
        description: { it: "La marina delle prove", en: "The test marina" },
        office_hours: [{ weekday: 1, from: "08:00", to: "18:00" }],
        depth_min_cm: 250,
        depth_max_cm: 600,
        shelter: ["N", "NE"],
        services: { water: true, wifi: true },
        show_map: true,
      },
    },
  );
  await adminRequest(page, "POST", `${marinaApi(marinaId)}/profile/publish`, {
    ifMatch: saved.data.version,
  });
}

async function expectNoSeriousViolations(page: Page, label: string) {
  const result = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  const serious = result.violations
    .filter((v) => v.impact === "serious" || v.impact === "critical")
    .map(
      (v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`,
    );
  expect(serious, `${label}: axe serious/critical violations`).toEqual([]);
}

test.describe("public marina profile", () => {
  let marina: OnlineMarina | null = null;

  test.afterEach(async ({ page }) => {
    if (marina) await archiveOpsHarbour(page, marina.marinaId);
    marina = null;
  });

  test("an anonymous visitor reads the profile, switches language and books", async ({
    page,
    browser,
  }) => {
    test.setTimeout(90_000);
    await login(page);
    marina = await setupOnlineMarina(page, { berths: 2, stripe: false });
    const { marinaId, slug } = marina;

    const anon = await browser.newContext({ locale: "it-IT" });
    const visitor = await anon.newPage();
    const cookies: string[] = [];
    visitor.on("response", (r) => {
      const c = r.headers()["set-cookie"];
      if (c && new URL(r.url()).pathname.match(/^\/(p|api\/public)\//))
        cookies.push(`${r.url()}: ${c}`);
    });

    // Not published yet: 404, noindex.
    const missing = await visitor.request.get(`/p/${slug}`);
    expect(missing.status()).toBe(404);
    expect(missing.headers()["x-robots-tag"]).toBe("noindex");

    await publishProfile(page, marinaId);

    const res = await visitor.goto(`/p/${slug}`);
    expect(res?.status()).toBe(200);
    expect(res?.headers()["cache-control"]).toBe("public, max-age=300");
    await expect(visitor.getByTestId("profile-name")).toHaveText(/Marina/);
    await expect(visitor.getByText("La marina delle prove")).toBeVisible();
    await expect(
      visitor.getByRole("link", { name: /\+39 050 1112222/ }),
    ).toHaveAttribute("href", "tel:+390501112222");
    await expect(visitor.getByTestId("shelter-rose")).toBeVisible();
    await expect(visitor.getByTestId("simple-map")).toBeVisible();

    // The head carries the canonical, hreflang alternates and exactly one JSON-LD Marina/Place.
    const html = await (await visitor.request.get(`/p/${slug}`)).text();
    expect(html).toContain(`/p/${slug}">`);
    expect(html).toContain('hreflang="en"');
    const scripts = visitor.locator('script[type="application/ld+json"]');
    await expect(scripts).toHaveCount(1);
    const ld = JSON.parse((await scripts.textContent()) ?? "{}") as Record<
      string,
      unknown
    >;
    expect(ld["@type"]).toEqual(["Marina", "Place"]);
    expect((ld.geo as Record<string, string>).latitude).toBe("43.7101234");
    expect((ld.potentialAction as Record<string, string>).target).toContain(
      `/b/${slug}`,
    );

    // No berth code on the page or in the simplified map.
    const mapJson = await (
      await visitor.request.get(`/api/public/v1/${slug}/profile/map`)
    ).text();
    expect(mapJson).not.toMatch(/"A-?\d+"/);
    expect(await visitor.content()).not.toMatch(/>A-?\d+</);

    await expectNoSeriousViolations(visitor, "public profile");

    // it/en switch.
    await visitor.getByTestId("profile-lang").getByText("English").click();
    await expect(visitor.getByText("The test marina")).toBeVisible();
    await expect(visitor).toHaveURL(/\?lang=en$/);
    await expect(
      visitor.getByRole("heading", { name: "Contacts" }),
    ).toBeVisible();

    // "Prenota" leads to the booking page.
    await visitor.getByTestId("profile-book").click();
    await expect(visitor).toHaveURL(new RegExp(`/b/${slug}(\\?lang=en)?$`));

    expect(cookies, "public responses must not set cookies").toEqual([]);
    await anon.close();

    // Withdrawn: 404 again.
    const cur = await adminRequest<Profile>(
      page,
      "GET",
      `${marinaApi(marinaId)}/profile`,
    );
    await adminRequest(
      page,
      "POST",
      `${marinaApi(marinaId)}/profile/unpublish`,
      { ifMatch: cur.data.version },
    );
    expect(
      (await page.request.get(`/api/public/v1/${slug}/profile`)).status(),
    ).toBe(404);
  });
});
