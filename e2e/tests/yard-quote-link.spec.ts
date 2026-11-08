import { expect, test } from "@playwright/test";

import { adminRequest, marinaApi } from "../support/map";
import { archiveOpsHarbour, setupOpsHarbour } from "../support/occupancy";
import { login } from "../support/session";
import { createAntifoulingQuote, setupYardCatalogue } from "../support/yard";

// Yard quote magic link (m10-plan §4.3): the quote sent from the panel opens on the customer page with its lines
// and total; the customer accepts the conditions and the quote, and the panel shows it accepted via link.

let marina: string | null = null;
test.afterEach(async ({ page }) => {
  if (marina) await archiveOpsHarbour(page, marina);
  marina = null;
});

test("yard quote link: the customer accepts from the link", async ({ page }) => {
  test.setTimeout(2 * 60_000);
  await login(page);
  const h = await setupOpsHarbour(page, { berths: 2 });
  marina = h.marinaId;
  const w = await setupYardCatalogue(page, h.marinaId);
  const q = await createAntifoulingQuote(page, h.marinaId, w, { conditions: "Saldo alla consegna." });
  const sent = await adminRequest<{ link_url: string }>(page, "POST", `${marinaApi(h.marinaId)}/yard/quotes/${q.id}/send`, {
    ifMatch: q.version,
    body: { manual: true },
  });
  const path = new URL(sent.data.link_url).pathname;

  await page.goto(path);
  const view = page.getByTestId("yard-quote-link");
  await expect(view).toContainText(q.number);
  await expect(page.getByTestId("yard-quote-link-line")).toHaveCount(6);
  await expect(page.getByTestId("yard-quote-link-gross")).toContainText("2.077,17");
  await page.getByLabel("Ho letto e accetto le condizioni").check();
  await page.getByRole("button", { name: "Accetta il preventivo" }).click();
  await expect(page.getByText("Preventivo accettato. Grazie!")).toBeVisible();

  await expect
    .poll(async () => {
      const r = await adminRequest<{ status: string; accepted_via?: string }>(
        page,
        "GET",
        `${marinaApi(h.marinaId)}/yard/quotes/${q.id}`,
      );
      return `${r.data.status}/${r.data.accepted_via ?? ""}`;
    })
    .toBe("accepted/link");
});
