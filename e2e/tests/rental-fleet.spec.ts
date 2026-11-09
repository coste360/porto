import { expect, test } from "@playwright/test";

import { selectOption } from "../support/pricing";
import {
  adminApi,
  dayFromToday,
  expectNoSeriousViolations,
  marinaApi,
  uniq,
} from "../support/registry";
import { login } from "../support/session";

// Rental fleet (m11-plan §14.1): a priced RIB (40 kW) is set up through the API and listed with the licence it needs;
// a unit is added from a registry vessel through the dialog (20 kW: no licence below the 30 kW threshold); a skipper
// gets the default weekly hours; the price coverage shows the hourly price of the RIB; the settings refuse a buffer
// wider than the gap between the bands before saving; the page has no serious accessibility finding.

type Doc = { id: string; version: number; settings: unknown };

test("rental fleet: units, skipper hours, price coverage and settings", async ({
  page,
}) => {
  test.setTimeout(2 * 60_000);
  await login(page);
  const tag = uniq();
  const api = marinaApi(page);
  const marina = api.split("/").at(-1);

  // A rental item priced 45.00 per hour, a 40 kW RIB unit and a 20 kW vessel, through the API.
  const { data: classes } = await adminApi<{
    items: { id: string; code: string }[];
  }>(page, "GET", `${api}/revenue-classes`);
  const rental = classes.items.find((c) => c.code === "rental")!.id;
  const { data: item } = await adminApi<{ id: string }>(
    page,
    "POST",
    `${api}/items`,
    {
      code: `NOL-F${tag}`,
      name: { it: `Noleggio ${tag}` },
      item_type: "service",
      unit: "hour",
      revenue_class_id: rental,
      price_includes_vat: true,
    },
  );
  const year = Number(dayFromToday(0).slice(0, 4));
  const list = {
    code: `NOL-F${tag}`,
    name: { it: `Noleggio ${tag}` },
    valid_from: `${year}-01-01`,
    valid_to: `${year + 1}-12-31`,
  };
  const { data: doc } = await adminApi<Doc>(
    page,
    "POST",
    `${api}/price-lists`,
    { ...list, contract_types: ["charter"] },
  );
  const { data: saved } = await adminApi<{ document: { version: number } }>(
    page,
    "PUT",
    `${api}/price-lists/${doc.id}`,
    {
      ...list,
      contract_types: [],
      settings: doc.settings,
      periods: [],
      bands: [],
      rules: [
        {
          key: "h",
          stage: "base",
          name: "Ora",
          item_id: item.id,
          basis: "flat",
          unit: "hour",
          amount_cents: 4500,
        },
      ],
    },
    { "If-Match": `"${doc.version}"` },
  );
  await adminApi(
    page,
    "POST",
    `${api}/price-lists/${doc.id}/activate`,
    {},
    { "If-Match": `"${saved.document.version}"` },
  );
  const { data: rib } = await adminApi<{ id: string }>(
    page,
    "POST",
    `${api}/vessels`,
    {
      name: `Gommone ${tag}`,
      loa_cm: 580,
      beam_cm: 240,
      hull_type: "rib",
      engines: [{ power_kw: 40 }],
    },
  );
  await adminApi(page, "POST", `${api}/rental/units`, {
    vessel_id: rib.id,
    code: `R${tag}`,
    category: "rib",
    seats: 8,
    fuel_type: "petrol",
    tank_milli: 60000,
    item_id: item.id,
  });
  await adminApi(page, "POST", `${api}/vessels`, {
    name: `Flotta ${tag}`,
    loa_cm: 500,
    beam_cm: 210,
    hull_type: "monohull_motor",
    engines: [{ power_kw: 20 }],
  });
  const { data: formulas } = await adminApi<{ items: { code: string }[] }>(
    page,
    "GET",
    `${api}/rental/formulas`,
  );
  if (!formulas.items.some((f) => f.code === "E2E-ORARIA")) {
    await adminApi(page, "POST", `${api}/rental/formulas`, {
      code: "E2E-ORARIA",
      name: { it: "Oraria" },
      kind: "hourly",
    });
  }

  await page.goto(`/admin/m/${marina}/rental/fleet`);
  await expect(
    page.getByTestId("unit-row").filter({ hasText: `R${tag}` }),
  ).toContainText("Patente richiesta");

  // New unit from the dialog.
  await page.getByTestId("unit-new").click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("combobox", { name: "Imbarcazione" }).click();
  await page.getByPlaceholder("Cerca imbarcazione").fill(`Flotta ${tag}`);
  await page.getByRole("option", { name: `Flotta ${tag}` }).click();
  await dialog.getByLabel("Codice").fill(`F${tag}`);
  await selectOption(page, dialog, "Categoria", "Open");
  await dialog.getByLabel("Serbatoio (litri)").fill("40");
  await selectOption(page, dialog, "Articolo", `NOL-F${tag}`);
  await dialog.getByTestId("unit-save").click();
  await expect(dialog).toBeHidden();
  const added = page.getByTestId("unit-row").filter({ hasText: `F${tag}` });
  await expect(added).toContainText("Senza patente");
  await expect(added).toContainText(`Flotta ${tag}`);

  // A skipper with the default weekly hours.
  await page.getByTestId("fleet-tab-skippers").click();
  await page.getByRole("button", { name: "Nuovo skipper" }).click();
  const sk = page.getByRole("dialog");
  await sk.getByLabel("Nome", { exact: true }).fill(`Skipper ${tag}`);
  await sk.getByRole("button", { name: "Salva" }).click();
  await expect(sk).toBeHidden();
  const skRow = page
    .getByTestId("skipper-row")
    .filter({ hasText: `Skipper ${tag}` });
  await skRow.getByRole("button", { name: "Orari" }).click();
  const hours = page.getByRole("dialog");
  await hours
    .getByRole("button", { name: "Lunedì-sabato, orario di apertura" })
    .click();
  await hours.getByRole("button", { name: "Salva" }).click();
  await expect(hours).toBeHidden();
  await expect(skRow).toContainText("Lun 08:00-19:00");

  // Price coverage: the hourly formula is priced on the RIB (the probe is the first hour of the window: 45,00 €).
  await page.getByTestId("fleet-tab-formulas").click();
  await expect(
    page.getByTestId("formula-row").filter({ hasText: "E2E-ORARIA" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Copertura prezzi" }).click();
  const cov = page.getByRole("dialog");
  await expect(
    cov.getByRole("row").filter({ hasText: "E2E-ORARIA" }),
  ).toContainText("45,00");
  await cov.getByRole("button", { name: "Chiudi" }).click();

  // Settings: a 3-slot buffer is wider than the 30-minute gap between the bands.
  await page.getByTestId("fleet-tab-settings").click();
  const settings = page.getByTestId("rental-settings");
  await expect(settings.getByTestId("band-strip")).toBeVisible();
  const buffer = settings.getByLabel("Margine tra noleggi (mezz'ore)");
  await buffer.fill("3");
  await expect(settings.getByTestId("window-issues")).toContainText(
    "Tra due fasce serve almeno il margine",
  );
  await expect(settings.getByTestId("settings-save")).toBeDisabled();
  await buffer.fill("1");
  await expect(settings.getByTestId("settings-save")).toBeEnabled();

  await expectNoSeriousViolations(page, "rental fleet");
});
