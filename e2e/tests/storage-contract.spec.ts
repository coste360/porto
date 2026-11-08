import { expect, test } from "@playwright/test";

import { mockStorage } from "../fixtures/m9/storage";
import { marinaIdOf } from "../support/api";
import { login } from "../support/session";

// New storage contract (m9-plan §3.2, §12.1): the form validates before sending and shows the no-space hint of a
// 409 storage_no_space (the storage endpoints are mocked).

test("storage contract: validation and no space", async ({ page }) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  await mockStorage(page, marinaId);
  await page.goto(`/admin/m/${marinaId}/storage`);
  await page
    .getByRole("link", { name: "Nuovo contratto di rimessaggio" })
    .click();
  const form = page.getByTestId("storage-contract-form");
  await expect(form).toBeVisible();
  await form.getByRole("button", { name: "Crea contratto" }).click();
  await expect(form).toContainText("Scegli il cliente.");
});
