import { expect, test } from "@playwright/test";

import { archiveOpsHarbour, marinaDay } from "../support/occupancy";
import {
  TINY_PNG,
  expectPhoneLayout,
  loginOnPhone,
  seedDockDay,
} from "../support/phone";
import { expectNoSeriousViolations } from "../support/registry";

// Dock phone screen (spec 31.3, m6b-plan §6.2, §10.2) on a Pixel 7, online: each tab; arrival with a note and a
// photo, departure from the map, cash collection, release, block and unexpected arrival.

let harbour: string | null = null;
test.afterEach(async ({ page }) => {
  if (harbour) await archiveOpsHarbour(page, harbour);
  harbour = null;
});

test("dock phone screen: today, map, cash and more, online", async ({
  page,
}) => {
  test.setTimeout(4 * 60_000);
  await loginOnPhone(page);
  const day = await seedDockDay(page);
  harbour = day.marinaId;
  await page.goto(`/admin/m/${day.marinaId}/dock`);
  const dock = page.getByTestId("dock-page");
  await expect(page.getByTestId("phone-shell")).toBeVisible();
  await expect(dock.getByRole("tab", { name: "Scansiona" })).toBeVisible();
  await expectPhoneLayout(page, "dock-page");

  // Oggi: Arrivato with a note and a photo (queued, sent at once while online).
  const arrivals = page.getByTestId("desk-movements-arrivals_expected");
  await arrivals
    .getByRole("listitem")
    .filter({ hasText: day.transit.vessel })
    .getByRole("button", { name: "Arrivato" })
    .click();
  const sheet = page.getByTestId("desk-presence-dialog");
  await sheet.getByLabel("Note").fill("Ormeggiato di poppa");
  await sheet
    .getByLabel("Foto")
    .setInputFiles({
      name: "arrivo.png",
      mimeType: "image/png",
      buffer: TINY_PNG,
    });
  await sheet.getByRole("button", { name: "Registra l'arrivo" }).click();
  await expect(sheet).toBeHidden();
  await expect(dock).toContainText("Arrivate: 1");
  await expect(page.getByTestId("offline-pending")).toHaveCount(0);
  const presences = await page.request.get(
    `/api/admin/v1/marinas/${day.marinaId}/presences?date=${marinaDay()}`,
  );
  const items = (
    (await presences.json()) as {
      items: { event: string; has_photo: boolean }[];
    }
  ).items;
  expect(items.filter((p) => p.event === "arrived")).toHaveLength(1);
  await expect
    .poll(async () => {
      const r = await page.request.get(
        `/api/admin/v1/marinas/${day.marinaId}/presences?limit=50`,
      );
      return (
        (await r.json()) as { items: { has_photo: boolean }[] }
      ).items.some((p) => p.has_photo);
    })
    .toBe(true);

  // Cassa (before the departure: an early departure reprices the stay): the open session and the transit's
  // charge, collected in cash in full.
  await dock.getByRole("tab", { name: "Cassa" }).click();
  await expect(page.getByText(/Cassa aperta dal/)).toBeVisible();
  const item = page
    .getByTestId(/^dock-collectible-/)
    .filter({ hasText: day.transit.vessel });
  await expect(item).toBeVisible();
  await item.getByRole("button", { name: "Incassa" }).click();
  const collect = page.getByTestId("dock-collect-sheet");
  await expect(
    collect.getByLabel("Importo incassato in contanti"),
  ).not.toHaveValue("");
  await collect.getByRole("button", { name: "Registra l'incasso" }).click();
  await expect(collect).toBeHidden();
  await expect(item).toHaveCount(0, { timeout: 15_000 });

  // Mappa: the day map in schema mode; tapping the transit's berth opens its sheet with Partito.
  await dock.getByRole("tab", { name: "Mappa" }).click();
  const canvas = page.getByTestId("phone-map-canvas");
  await expect(canvas).toBeVisible();
  await expect(page.getByTestId("phone-map-legend")).toBeVisible();
  await page.getByTestId("phone-map-context-payments").click();
  await expect(canvas).toHaveAttribute("data-context", "payments");
  await page.getByTestId("phone-map-context-presence").click();
  // The presence legend names its entries (no bare "Nessuna"): a boat without a recorded presence is labelled.
  await expect(page.getByTestId("phone-map-legend-none")).toHaveCount(0);
  const berth = canvas.getByRole("button", {
    name: new RegExp(`^${day.berths[1]!.code},`),
  });
  await berth.focus();
  await page.keyboard.press("Enter");
  const element = page.getByTestId("phone-element-sheet");
  await expect(element).toContainText(day.transit.vessel);
  await element.getByRole("button", { name: "Partito" }).click();
  await page
    .getByTestId("desk-presence-dialog")
    .getByRole("button", { name: "Registra la partenza" })
    .click();
  await expect(page.getByTestId("desk-presence-dialog")).toBeHidden();

  // Altro: release of the annual berth, block of a free berth, unexpected arrival (all online).
  await dock.getByRole("tab", { name: "Altro" }).click();
  await page.getByTestId("dock-more-releases").click();
  await page
    .getByLabel("Cerca il contratto per barca o codice")
    .fill(day.annual.vessel);
  await page
    .getByRole("button", { name: new RegExp(day.annual.vessel) })
    .click();
  const release = page.getByRole("dialog");
  await expect(release.getByText("Rilascia notti")).toBeVisible();
  await release.getByRole("button", { name: "Rilascia", exact: true }).click();
  await expect(release).toBeHidden();
  await page.getByRole("combobox", { name: "Posto o risorsa" }).click();
  await page
    .getByRole("option", { name: new RegExp(`^${day.berths[3]!.code} ·`) })
    .click();
  await page.getByRole("button", { name: "Blocca il posto" }).click();
  const block = page.getByRole("dialog");
  await block.getByLabel("Motivo").fill("Lavori al pontile");
  await block.getByRole("button", { name: "Blocca", exact: true }).click();
  await expect(block).toBeHidden();
  await page.getByRole("button", { name: "Indietro" }).click();
  await page.getByTestId("dock-more-unexpected").click();
  await page.getByLabel("Nome dell'imbarcazione").fill(`Libeccio ${day.tag}`);
  await page.getByLabel("Lunghezza fuori tutto").fill("8,5");
  await page.getByLabel("Larghezza", { exact: true }).fill("3");
  await page.getByRole("combobox", { name: "Posto o risorsa" }).click();
  await page
    .getByRole("option", { name: new RegExp(`^${day.berths[2]!.code} ·`) })
    .click();
  await page.getByRole("button", { name: "Registra l'arrivo" }).click();
  await expect(page.getByText("Arrivo registrato")).toBeVisible();

  await expectNoSeriousViolations(page, "phone dock");
});
