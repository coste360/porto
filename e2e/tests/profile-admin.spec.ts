import path from "node:path";

import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

import { adminRequest, marinaApi } from "../support/map";
import { archiveOpsHarbour, setupOpsHarbour } from "../support/occupancy";
import { login } from "../support/session";

// M12 "Scheda pubblica" panel (m12-plan §16.2): fill the draft, upload a photo, publish, open the preview link and
// withdraw. A fresh marina keeps the shared owner marina untouched.

const PHOTO = path.join(
  import.meta.dirname,
  "..",
  "fixtures",
  "m8",
  "insurance-photo.png",
);

test.describe("public profile panel", () => {
  let marinaId: string | null = null;

  test.afterEach(async ({ page }) => {
    if (marinaId) await archiveOpsHarbour(page, marinaId);
    marinaId = null;
  });

  test("the owner fills, publishes and withdraws the profile", async ({
    page,
  }) => {
    test.setTimeout(90_000);
    await login(page);
    marinaId = (await setupOpsHarbour(page, { berths: 2, linearAreaM: 10 }))
      .marinaId;
    await page.goto(`/admin/m/${marinaId}/profile`);

    await expect(page.getByTestId("profile-status")).toHaveText(
      "Non pubblicata",
    );
    await expect(page.getByTestId("profile-missing")).toContainText(
      "la posizione dell'imboccatura",
    );
    await expect(page.getByTestId("profile-publish")).toBeDisabled();

    // General: description per language and the position in decimal degrees.
    await page.getByLabel("Descrizione (Italiano)").fill("Porto delle prove");
    await page.getByLabel("Latitudine").fill("43,7101234");
    await page.getByLabel("Longitudine").fill("10.4012345");
    // Contacts.
    await page.getByRole("tab", { name: "Contatti" }).click();
    await page.getByLabel("Telefono").fill("+39 050 1112222");
    await page.getByLabel("Sito web").fill("http://insicuro.example");
    await page.getByTestId("profile-save").click();
    await expect(page.getByTestId("profile-form-invalid")).toBeVisible();
    await page.getByLabel("Sito web").fill("https://marina-e2e.example");
    // Harbour: depths in metres.
    await page.getByRole("tab", { name: "Porto" }).click();
    await page.getByLabel("Fondale minimo").fill("2,50");
    await page.getByLabel("Fondale massimo").fill("6");
    // Photos: upload, alt text.
    await page.getByRole("tab", { name: "Foto" }).click();
    await page.getByTestId("profile-photos-upload").setInputFiles(PHOTO);
    await expect(page.getByTestId("profile-photos").locator("img")).toHaveCount(
      1,
    );
    await page
      .getByLabel("Testo alternativo (Italiano)")
      .first()
      .fill("Ingresso del porto");

    const saved = page.waitForResponse(
      (r) => r.url().endsWith("/profile") && r.request().method() === "PUT",
    );
    await page.getByTestId("profile-save").click();
    expect((await saved).status()).toBe(200);
    await expect(page.getByTestId("profile-missing")).toHaveCount(0);

    const draft = await adminRequest<{
      entrance_lat_e7: number;
      depth_min_cm: number;
      depth_max_cm: number;
      photos: { file_id: string }[];
    }>(page, "GET", `${marinaApi(marinaId)}/profile`);
    expect(draft.data.entrance_lat_e7).toBe(437101234);
    expect(draft.data.depth_min_cm).toBe(250);
    expect(draft.data.depth_max_cm).toBe(600);
    expect(draft.data.photos).toHaveLength(1);

    // Preview link of the draft, then publish.
    await expect(page.getByTestId("profile-preview")).toHaveAttribute(
      "href",
      new RegExp(`/p/[a-z0-9-]+\\?preview=1&marina=${marinaId}$`),
    );
    const href =
      (await page.getByTestId("profile-preview").getAttribute("href")) ?? "";
    const preview = await page.context().newPage();
    await preview.goto(href);
    await expect(preview.getByTestId("profile-preview-banner")).toBeVisible();
    await expect(preview.getByText("Porto delle prove")).toBeVisible();
    await preview.close();

    await page.getByTestId("profile-publish").click();
    await expect(page.getByTestId("profile-status")).toHaveText("Pubblicata");
    const url = (await page.getByTestId("profile-url").textContent()) ?? "";
    const slug = url.split("/p/")[1] ?? "";
    expect(
      (await page.request.get(`/api/public/v1/${slug}/profile`)).status(),
    ).toBe(200);

    // Toasts fade over the page: axe runs once they are gone.
    await expect(page.locator("[data-sonner-toast]")).toHaveCount(0, {
      timeout: 15_000,
    });
    const axe = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa"])
      .analyze();
    expect(
      axe.violations
        .filter((v) => v.impact === "serious" || v.impact === "critical")
        .map(
          (v) =>
            `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`,
        ),
    ).toEqual([]);

    await page.getByTestId("profile-unpublish").click();
    await expect(page.getByTestId("profile-status")).toHaveText(
      "Non pubblicata",
    );
    expect(
      (await page.request.get(`/api/public/v1/${slug}/profile`)).status(),
    ).toBe(404);
  });
});
