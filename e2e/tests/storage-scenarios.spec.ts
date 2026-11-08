import { expect, test } from "@playwright/test";

import {
  AREA_ID,
  SCENARIO_ID,
  mockStorage,
  scenario,
} from "../fixtures/m9/storage";
import { marinaIdOf } from "../support/api";
import { login } from "../support/session";

// Storage scenarios (m9-plan §7, §12.1, §14.5): list, editor with the moves, compare page, apply with the
// conflict path (409 lists every conflict, nothing applied) and the successful path to the movements.

test("storage scenarios: compare and apply with conflicts", async ({
  page,
}) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  const calls = await mockStorage(page, marinaId);

  await page.goto(`/admin/m/${marinaId}/storage/${AREA_ID}/scenarios`);
  await page.getByRole("link", { name: "Inverno E2E" }).click();
  await expect(page.getByTestId("scenario-moves")).toContainText("Bora");

  await page.getByRole("link", { name: "Confronta" }).click();
  await expect(page.getByTestId("compare-indicators")).toBeVisible();
  await page.getByRole("link", { name: "Torna all'ipotesi" }).click();

  await page.getByTestId("scenario-apply").click();
  await page
    .getByRole("alertdialog")
    .getByRole("button", { name: "Applica" })
    .click();
  const conflicts = page.getByTestId("scenario-conflicts");
  await expect(conflicts).toContainText("2 conflitti");
  await expect(conflicts).toContainText("Aria: cambiata dopo il caricamento");
  const apply = calls.find(
    (c) => c.path === `/storage/scenarios/${SCENARIO_ID}/apply`,
  )!;
  expect(apply.headers["if-match"]).toBe('"4"');
});

test("storage scenarios: a successful apply opens the movements", async ({
  page,
}) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  await mockStorage(page, marinaId, {
    apply: {
      status: 200,
      body: {
        scenario: { ...scenario().scenario, status: "applied" },
        movements: [],
      },
    },
  });
  await page.goto(
    `/admin/m/${marinaId}/storage/${AREA_ID}/scenarios/${SCENARIO_ID}`,
  );
  await page.getByTestId("scenario-apply").click();
  await page
    .getByRole("alertdialog")
    .getByRole("button", { name: "Applica" })
    .click();
  await expect(page.getByTestId("storage-movements")).toBeVisible();
});

test("storage scenarios: a boat of another area is added from the tray", async ({
  page,
}) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  const calls = await mockStorage(page, marinaId);
  await page.goto(
    `/admin/m/${marinaId}/storage/${AREA_ID}/scenarios/${SCENARIO_ID}`,
  );
  const tray = page.getByTestId("storage-tray-candidates");
  await expect(tray).toContainText("Grecale");
  await expect(tray).toContainText("PIAZZALE");
  await tray
    .getByRole("button", { name: "Aggiungi Grecale all'ipotesi" })
    .click();
  await expect
    .poll(() =>
      calls.find(
        (c) =>
          c.method === "POST" &&
          c.path === `/storage/scenarios/${SCENARIO_ID}/items`,
      ),
    )
    .toBeTruthy();
  const add = calls.find(
    (c) =>
      c.method === "POST" &&
      c.path === `/storage/scenarios/${SCENARIO_ID}/items`,
  )!;
  expect(add.body).toEqual({ allocation_id: "al-yard" });
  expect(add.headers["if-match"]).toBe('"4"');
});
