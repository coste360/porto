import { expect, test, type Page } from "@playwright/test";

import { adminApi, createVessel, dayFromToday, marinaApi, uniq } from "../support/registry";
import { login } from "../support/session";

// Lifting machines (m10-plan §11, INV-17): a travel lift booked from a free slot of the day calendar, then the
// same slot asked again through the booking dialog: the server answers 409 equipment_slot_taken and the dialog
// names the taken time; a different time is accepted. Everything runs against the real backend.

test("yard lifting: book a slot and see the conflict on the same slot", async ({ page }) => {
  await login(page);
  const api = marinaApi(page);
  const tag = uniq();
  const { data: eq } = await adminApi<{ id: string; code: string }>(page, "POST", `${api}/equipment`, {
    code: `TL-${tag}`,
    name: `Travel lift ${tag}`,
    kind: "travel_lift",
    capacity_kg: 70000,
  });
  const vessel = await createVessel(page, { name: `Maestrale ${tag}`, loa_cm: 1200, beam_cm: 400, hull_type: "monohull_sail" });
  const day = dayFromToday(1);
  // 10:00-10:30 is taken through the API.
  await adminApi(page, "POST", `${api}/equipment-bookings`, {
    equipment_id: eq.id,
    date: day,
    from_slot: 40,
    to_slot: 42,
    purpose: "haul_out",
    vessel_id: vessel.id,
  });

  const marina = api.split("/").at(-1);
  await page.goto(`/admin/m/${marina}/yard/lifting`);
  await page.getByLabel("Giorno").first().fill(day);
  const grid = page.getByTestId("lifting-grid");
  await expect(grid.getByTestId("lifting-booking").filter({ hasText: `Maestrale ${tag}` })).toBeVisible();

  // Book 08:00 from the free slot.
  await page.getByTestId(`lifting-free-TL-${tag}-08:00`).click();
  let dialog = page.getByTestId("booking-dialog");
  // A haul-out needs its vessel: "Prenota" stays disabled until one is chosen.
  await expect(dialog.getByRole("button", { name: "Prenota" })).toBeDisabled();
  await pickVessel(page, `Maestrale ${tag}`);
  await dialog.getByRole("button", { name: "Prenota" }).click();
  await expect(dialog).toBeHidden();
  await expect(grid.getByTestId("lifting-booking")).toHaveCount(2);

  // The same slot as the API booking, through the dialog: 409 with the taken time.
  await page.getByRole("button", { name: "Prenota mezzo" }).click();
  dialog = page.getByTestId("booking-dialog");
  await dialog.locator("#lift-equipment").click();
  await page.getByRole("option", { name: new RegExp(`TL-${tag}`) }).click();
  await pickVessel(page, `Maestrale ${tag}`);
  await dialog.locator("#lift-from").click();
  await page.getByRole("option", { name: "10:00", exact: true }).click();
  await dialog.getByRole("button", { name: "Prenota" }).click();
  await expect(dialog.getByTestId("booking-conflict")).toContainText("10:00-10:30");
  // Another time is free.
  await dialog.locator("#lift-from").click();
  await page.getByRole("option", { name: "11:00", exact: true }).click();
  await dialog.getByRole("button", { name: "Prenota" }).click();
  await expect(dialog).toBeHidden();
  await expect(grid.getByTestId("lifting-booking")).toHaveCount(3);
});

async function pickVessel(page: Page, name: string) {
  const dialog = page.getByTestId("booking-dialog");
  await dialog.locator("#lift-vessel-q").fill(name);
  await dialog.locator("#lift-vessel").click();
  await page.getByRole("option", { name }).click();
}
