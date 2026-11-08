import { expect, test } from "@playwright/test";

import { login } from "../support/session";
import { expectNoSeriousViolations } from "../support/registry";

// Period comparison (spec 21.3, m7-plan §5, §11.6): indicator, years back, alignment and granularity in the URL;
// the totals row equals the sum of the series; CSV export starts with the BOM and the Italian header.

test("comparison: controls, totals and CSV export", async ({ page }) => {
  await login(page);
  const marinaPath = new URL(page.url()).pathname.replace(/\/$/, "");
  const year = new Date().getFullYear();
  await page.goto(
    `${marinaPath}/reports/comparison?indicator=production&from=${year}-01-01&to=${year}-03-31&granularity=month`,
  );
  const view = page.getByTestId("comparison-page");
  await expect(view).toBeVisible();
  await expect(page.getByTestId("comparison-row")).toHaveCount(3);
  await expect(page.getByTestId("comparison-total")).toContainText("Totale");

  await page.getByRole("radio", { name: "Per giorno della settimana" }).click();
  await expect(page).toHaveURL(/align=weekday/);
  await page.getByRole("combobox", { name: "Anni a confronto" }).click();
  await page.getByRole("option", { name: "2 anni" }).click();
  await expect(page).toHaveURL(/years_back=2/);
  await expect(page.getByTestId("comparison-row")).toHaveCount(3);

  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Esporta CSV" }).click();
  const file = await download;
  expect(file.suggestedFilename()).toBe(
    `porto-comparison-${year}-01-01-${year}-03-31.csv`,
  );
  const stream = await file.createReadStream();
  const chunks: Buffer[] = [];
  for await (const c of stream) chunks.push(c as Buffer);
  const csv = Buffer.concat(chunks).toString("utf8");
  expect(csv.startsWith("\ufeffPeriodo;Valore;Valore anno precedente")).toBe(
    true,
  );
  expect(csv.trimEnd().split("\r\n")).toHaveLength(5);

  await expectNoSeriousViolations(page, "comparison");
});
