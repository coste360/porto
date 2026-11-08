import { expect, test } from "@playwright/test";

import { setupOnlineMarina, type OnlineMarina } from "../fixtures/m8/setup";
import { OWNER } from "../support/env";
import { adminRequest, marinaApi } from "../support/map";
import {
  archiveOpsHarbour,
  createTransitViaApi,
  createVesselViaApi,
  marinaDay,
} from "../support/occupancy";
import { login } from "../support/session";

test("a marina with online booking off shows the coming-soon notice", async ({
  page,
}) => {
  await page.goto(`/b/${OWNER.slug}`);
  await expect(page.getByRole("heading", { name: OWNER.marina })).toBeVisible();
  await expect(
    page.getByText("La prenotazione online arriverà presto"),
  ).toBeVisible();
  await expect(page.getByText("powered by Porto")).toBeVisible();
});

test("an unknown marina shows the not-found page", async ({ page }) => {
  await page.goto("/b/does-not-exist");
  await expect(page.getByText(OWNER.marina)).toHaveCount(0);
  await expect(
    page.getByText("Controlla l'indirizzo della pagina."),
  ).toBeVisible();
});

test("?lang=en switches the booking page to English", async ({ page }) => {
  await page.goto(`/b/${OWNER.slug}?lang=en`);
  await expect(page.locator("html")).toHaveAttribute("lang", "en");
});

test.describe("online booking search", () => {
  let marina: OnlineMarina | null = null;

  test.afterEach(async ({ page }) => {
    if (marina) await archiveOpsHarbour(page, marina.marinaId);
    marina = null;
  });

  test("a search with no free berth shows the empty state and becomes lost demand in R4", async ({
    page,
  }) => {
    test.setTimeout(90_000);
    await login(page);
    marina = await setupOnlineMarina(page, { berths: 2, stripe: false });
    const { marinaId, slug, berthIds } = marina;
    const arrival = marinaDay(10);
    const departure = marinaDay(12);
    // Both online berths are taken at the desk for those nights.
    for (const berthId of berthIds) {
      const vessel = await createVesselViaApi(page, marinaId, {
        loaCm: 1000,
        beamCm: 300,
      });
      await createTransitViaApi(page, marinaId, {
        vesselId: vessel.id,
        berthId,
        arrival,
        departure,
      });
    }

    // A free search on other dates finds the category.
    await page.goto(`/b/${slug}`);
    await page.getByLabel("Arrivo").fill(marinaDay(20));
    await page.getByLabel("Partenza").fill(marinaDay(22));
    await page.getByLabel("Lunghezza fuori tutto").fill("11,00");
    await page.getByLabel("Larghezza").fill("3,50");
    await page.getByRole("button", { name: "Cerca" }).click();
    await expect(page.getByTestId("booking-category").first()).toBeEnabled();

    // The full dates show the empty state.
    await page.getByRole("button", { name: "Modifica" }).click();
    await page.getByLabel("Arrivo").fill(arrival);
    await page.getByLabel("Partenza").fill(departure);
    await page.getByRole("button", { name: "Cerca" }).click();
    await expect(page.getByTestId("booking-empty")).toBeVisible();
    await expect(page.getByText("Nessun posto disponibile")).toBeVisible();
    await expect(page).toHaveURL(/#berth$/);

    // The unanswered search is an anonymous online inquiry in the lost demand report (R4).
    await expect
      .poll(
        async () => {
          const r = await adminRequest<{
            records: { channel: string; arrival: string; loa_cm: number }[];
          }>(
            page,
            "GET",
            `${marinaApi(marinaId)}/reports/lost-demand?from=${arrival}&to=${departure}`,
          );
          return r.data.records.filter(
            (x) => x.channel === "online" && x.arrival === arrival,
          ).length;
        },
        { timeout: 15_000 },
      )
      .toBeGreaterThanOrEqual(1);
  });
});
