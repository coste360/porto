import { expect, test, type Locator, type Page } from "@playwright/test";

import { adminApi, dayFromToday, expectNoSeriousViolations, marinaApi, uniq } from "../support/registry";
import { login } from "../support/session";

// Rental planner (m11-plan §14.2, INV-18): a priced unit is created through the API, then on the hours view a drag
// on free cells opens the booking dialog with the hourly formula suggested; "Opzione" holds the slots, the booking
// page confirms it, a drag moves it two hours later, and a drag over it is refused by the server (409
// rental_slot_taken) with the conflicting booking highlighted on the planner.

type Item = { id: string };
type Doc = { id: string; version: number; settings: unknown };

async function rentalFleet(page: Page, tag: string): Promise<{ unitCode: string }> {
  const api = marinaApi(page);
  const { data: classes } = await adminApi<{ items: { id: string; code: string }[] }>(page, "GET", `${api}/revenue-classes`);
  const rental = classes.items.find((c) => c.code === "rental")!.id;
  const { data: item } = await adminApi<Item>(page, "POST", `${api}/items`, {
    code: `NOL-E2E-${tag}`,
    name: { it: `Gommone ${tag}` },
    item_type: "service",
    unit: "hour",
    revenue_class_id: rental,
    price_includes_vat: true,
  });
  const year = dayFromToday(0).slice(0, 4);
  const next = String(Number(year) + 1);
  const { data: doc } = await adminApi<Doc>(page, "POST", `${api}/price-lists`, {
    code: `NOL-E2E-${tag}`,
    name: { it: `Noleggio ${tag}` },
    valid_from: `${year}-01-01`,
    valid_to: `${next}-12-31`,
    contract_types: ["charter"],
  });
  const { data: saved } = await adminApi<{ document: { version: number } }>(
    page,
    "PUT",
    `${api}/price-lists/${doc.id}`,
    {
      code: `NOL-E2E-${tag}`,
      name: { it: `Noleggio ${tag}` },
      valid_from: `${year}-01-01`,
      valid_to: `${next}-12-31`,
      contract_types: [],
      settings: doc.settings,
      periods: [],
      bands: [],
      rules: [{ key: "h", stage: "base", name: "Ora", item_id: item.id, basis: "flat", unit: "hour", amount_cents: 4500 }],
    },
    { "If-Match": `"${doc.version}"` },
  );
  await adminApi(page, "POST", `${api}/price-lists/${doc.id}/activate`, {}, { "If-Match": `"${saved.document.version}"` });
  const { data: vessel } = await adminApi<Item>(page, "POST", `${api}/vessels`, {
    name: `Gommone ${tag}`,
    loa_cm: 580,
    beam_cm: 240,
    hull_type: "rib",
  });
  const unitCode = `G${tag}`;
  await adminApi(page, "POST", `${api}/rental/units`, {
    vessel_id: vessel.id,
    code: unitCode,
    category: "rib",
    seats: 6,
    fuel_type: "petrol",
    tank_milli: 60000,
    item_id: item.id,
  });
  const { data: formulas } = await adminApi<{ items: { code: string }[] }>(page, "GET", `${api}/rental/formulas`);
  if (!formulas.items.some((f) => f.code === "E2E-ORARIA")) {
    await adminApi(page, "POST", `${api}/rental/formulas`, { code: "E2E-ORARIA", name: { it: "Oraria" }, kind: "hourly" });
  }
  return { unitCode };
}

/** Drags with the mouse from the centre of one element to the centre of another. */
async function drag(page: Page, from: Locator, to: Locator) {
  const a = (await from.boundingBox())!;
  const b = (await to.boundingBox())!;
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2);
  await page.mouse.down();
  await page.mouse.move(a.x + a.width / 2 + 2, a.y + a.height / 2, { steps: 2 });
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2, { steps: 6 });
  await page.mouse.up();
}

test("rental planner: drag to hold, confirm, move and a refused overlap", async ({ page }) => {
  await login(page);
  const tag = uniq();
  const { unitCode } = await rentalFleet(page, tag);
  const marina = marinaApi(page).split("/").at(-1);
  const day = dayFromToday(40);
  await page.goto(`/admin/m/${marina}/rental/planner`);
  await page.getByLabel("Dal").fill(day);
  const cell = (i: number) => page.getByTestId(`planner-cell-${unitCode}-${i}`);
  // Hours view: cells start at 06:00, so cells 8..11 are 10:00-12:00.
  await expect(cell(8)).toBeVisible();

  // Drag 10:00-12:00: the dialog suggests the hourly formula priced 2 x 45.00.
  await drag(page, cell(8), cell(11));
  let dialog = page.getByTestId("rental-booking-dialog");
  await expect(dialog).toContainText(`${unitCode}, 10:00-12:00`);
  // The hourly formula is preselected (the press covered four 30-minute slots).
  await expect(dialog.getByRole("radio", { checked: true })).toHaveAccessibleName(/ORARIA · 90,00/);
  await dialog.getByLabel("Nome del cliente").fill("Ada Rossi");
  await dialog.getByRole("button", { name: "Opzione" }).click();
  await expect(dialog).toBeHidden();
  const booking = page.getByRole("button", { name: new RegExp(`${unitCode}: NL\\d{4}-\\d{6} In opzione`) });
  await expect(booking).toBeVisible();

  // Open it and confirm the option.
  await booking.click();
  await expect(page.getByTestId("rental-booking-status")).toHaveText("In opzione");
  await page.getByRole("button", { name: "Conferma" }).click();
  await expect(page.getByTestId("rental-booking-status")).toHaveText("Confermato");
  await page.getByRole("button", { name: "Planner noleggi" }).click();
  await page.getByLabel("Dal").fill(day);

  // Move it two hours later (10:00 -> 12:00): pressed at its centre (11:00, cell 10), released on 13:00 (cell 14).
  const confirmed = page.getByRole("button", { name: new RegExp(`${unitCode}: NL\\d{4}-\\d{6} Confermato, 10:00-12:00`) });
  await expect(confirmed).toBeVisible();
  await drag(page, confirmed, cell(14));
  await expect(page.getByRole("button", { name: new RegExp(`${unitCode}: NL\\d{4}-\\d{6} Confermato, 12:00-14:00`) })).toBeVisible();

  // An overlapping drag (13:30-15:00, pressed on the free 14:30 cell and dragged back over the booking) is refused by
  // the server and the booking in the way is highlighted.
  await drag(page, cell(17), cell(15));
  dialog = page.getByTestId("rental-booking-dialog");
  await expect(dialog.getByRole("radio", { checked: true })).toBeVisible();
  await dialog.getByRole("button", { name: "Conferma" }).click();
  await expect(dialog.getByTestId("rental-booking-conflict")).toBeVisible();
  await dialog.getByRole("button", { name: "Chiudi" }).click();
  await expect(
    page.locator('[data-testid="planner-booking"][data-status="confirmed"] rect.stroke-destructive'),
  ).toHaveCount(1);
  // The planner page has no serious or critical accessibility finding (axe).
  await expectNoSeriousViolations(page, "rental planner");
});
