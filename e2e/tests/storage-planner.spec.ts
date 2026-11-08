import { expect, test } from "@playwright/test";

import { AREA_ID, addDays, mockStorage, today } from "../fixtures/m9/storage";
import { marinaIdOf } from "../support/api";
import { login } from "../support/session";

// Storage planner (m9-plan §12.1, §14.5): the plan at scale with the Accessibilità lens, a boat dragged with the
// pointer (ghost, snap to cells), the draft evaluated by the server, Ripristina, Preset and Salva through the
// layout save. The storage endpoints are mocked (fixtures/m9/storage).

test("storage planner: drag, restore, preset and save", async ({ page }) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  const calls = await mockStorage(page, marinaId);

  await page.goto(`/admin/m/${marinaId}/storage`);
  await page.getByRole("link", { name: "Capannone E2E" }).click();
  const plan = page.getByTestId("storage-plan");
  await expect(plan).toBeVisible();
  // The lens shows the accessibility of one date: the day Aria is due out (fixture: today + 2).
  await page.goto(
    `/admin/m/${marinaId}/storage/${AREA_ID}?date=${addDays(today(), 2)}`,
  );
  await expect(plan).toBeVisible();
  const aria = plan.locator('[data-allocation="al-a"]');
  await expect(aria).toHaveAttribute("data-lens", "blocked");
  await expect(plan.locator('[data-allocation="al-b"]')).toHaveAttribute(
    "data-lens",
    "blocking",
  );

  // Drag Aria by a few cells: the ghost follows, the drop snaps and the draft is evaluated.
  const box = (await aria.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(
    box.x + box.width / 2 + 80,
    box.y + box.height / 2 + 60,
    { steps: 5 },
  );
  await expect(page.getByTestId("storage-drag-ghost")).toBeVisible();
  await page.mouse.up();
  await expect
    .poll(() => calls.some((c) => c.path.endsWith("/evaluate")))
    .toBe(true);
  const save = page.getByTestId("storage-save");
  await expect(save).toBeEnabled();

  await page.getByRole("button", { name: "Ripristina" }).click();
  await expect(save).toBeDisabled();

  await page.getByRole("button", { name: "Preset" }).click();
  await expect(save).toHaveText(/Salva \(1\)/);
  await save.click();
  await expect
    .poll(
      () =>
        calls.find((c) => c.path === `/dynamic-areas/${AREA_ID}/layout`)?.body,
    )
    .toMatchObject({
      placements: [
        { allocation_id: "al-a", x_cm: 1200, y_cm: 100, rotation_dd: 0 },
      ],
    });
});

test("storage planner: keyboard moves and rotation", async ({ page }) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  const calls = await mockStorage(page, marinaId);
  await page.goto(`/admin/m/${marinaId}/storage/${AREA_ID}`);
  const aria = page
    .getByTestId("storage-plan")
    .locator('[data-allocation="al-a"]');
  await aria.focus();
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("r");
  await expect
    .poll(() => {
      const ev = calls.filter((c) => c.path.endsWith("/evaluate")).at(-1)
        ?.body as
        | {
            items: {
              allocation_id: string;
              x_cm: number;
              rotation_dd: number;
            }[];
          }
        | undefined;
      return ev?.items.find((i) => i.allocation_id === "al-a");
    })
    .toMatchObject({ x_cm: 100, rotation_dd: 900 });
});
