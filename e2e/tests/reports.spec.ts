import { expect, test } from "@playwright/test";

import { marinaIdOf } from "../support/api";
import {
  billingDay,
  createBillingCustomer,
  createBillingItem,
  createManualCharge,
} from "../support/billing";
import { login } from "../support/session";

// M7 reports (m7-plan §7, §10.1, §11.6): the index lists the reports, a report page keeps its filter in the URL,
// pages its records, shows a Totale equal to the totals, and downloads Italian CSV (BOM, ';', decimal comma)
// and Excel files of the same request.

async function readDownload(
  download: import("@playwright/test").Download,
): Promise<Buffer> {
  const stream = await download.createReadStream();
  const chunks: Buffer[] = [];
  for await (const c of stream) chunks.push(c as Buffer);
  return Buffer.concat(chunks);
}

test("reports: index, filter, paginate, Totale and CSV/Excel export", async ({
  page,
}) => {
  await login(page);
  const marinaId = marinaIdOf(page);
  const item = await createBillingItem(page.request, marinaId);
  const customer = await createBillingCustomer(page.request, marinaId);
  // Three charges today: with page_size=2 the records span two pages.
  for (let i = 0; i < 3; i++) {
    await createManualCharge(
      page.request,
      marinaId,
      customer.id,
      item.id,
      1000,
      12_200,
    );
  }

  await page.goto(`/admin/m/${marinaId}/reports`);
  await expect(page.getByTestId("reports-index")).toBeVisible();
  await page.getByTestId("report-card-charges").click();
  await expect(page).toHaveURL(/\/reports\/charges/);
  await expect(page.getByTestId("report-page")).toBeVisible();

  const today = billingDay();
  await page.goto(
    `/admin/m/${marinaId}/reports/charges?from=${today}&to=${today}&page_size=2&origin=manual&customer_id=${customer.id}`,
  );
  await expect(page.getByTestId("report-record")).toHaveCount(2);
  await expect(page.getByTestId("report-pager")).toContainText("Pagina 1 di 2");
  await page.getByRole("button", { name: "Successiva" }).click();
  await expect(page).toHaveURL(/page=2/);
  await expect(page.getByTestId("report-record")).toHaveCount(1);

  // The Totale cell is the totals of the response, also shown as the summary card.
  const totalGross = page.getByTestId("report-total-cell-gross_cents");
  await expect(totalGross).toContainText("366,00");
  await expect(page.getByTestId("report-total-gross_cents")).toHaveText(
    (await totalGross.textContent()) ?? "",
  );

  // A filter change goes back to the first page and stays in the URL.
  await page.getByRole("combobox", { name: "Raggruppa per" }).first().click();
  await page.getByRole("option", { name: "Cliente" }).click();
  await expect(page).toHaveURL(/group_by=customer/);
  await expect(page).not.toHaveURL(/page=2/);
  await expect(page.getByTestId("report-groups")).toBeVisible();

  const csvDownload = page.waitForEvent("download");
  await page.getByRole("button", { name: "Esporta CSV" }).click();
  const csvFile = await csvDownload;
  expect(csvFile.suggestedFilename()).toBe(
    `porto-charges-${today}-${today}.csv`,
  );
  const csv = (await readDownload(csvFile)).toString("utf8");
  expect(csv.startsWith("﻿Data emissione;Numero;")).toBe(true);
  const lines = csv.trimEnd().split("\r\n");
  // Header, every record (export ignores the page), Totale.
  expect(lines).toHaveLength(1 + 3 + 1);
  expect(lines.at(-1)).toMatch(/^Totale;/);
  expect(lines.at(-1)).toContain(";366,00;");

  const xlsxDownload = page.waitForEvent("download");
  await page.getByRole("button", { name: "Esporta Excel" }).click();
  const xlsxFile = await xlsxDownload;
  expect(xlsxFile.suggestedFilename()).toBe(
    `porto-charges-${today}-${today}.xlsx`,
  );
  const xlsx = await readDownload(xlsxFile);
  expect(xlsx.subarray(0, 2).toString("latin1")).toBe("PK");
});
