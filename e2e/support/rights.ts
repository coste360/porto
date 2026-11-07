import type { Page } from '@playwright/test';

import { createCustomerWithEmail } from './contractsM4';
import { adminRequest, marinaApi } from './map';
import { marinaDay, setupOpsHarbour } from './occupancy';

// M4 rights helpers for the Playwright specs (m4-plan §9.5; signatures fixed). Everything goes through the admin
// API with the page session: a fresh marina (M3 setupOpsHarbour), synthetic holders with e2e emails.

const uniq = () => Date.now().toString(36).slice(-5) + Math.floor(Math.random() * 1e4).toString(36);

/**
 * Creates a marina with `berths` (default 40) berths and a right of use on every fourth berth: odd rights have
 * one holder (1000 ‰), even rights two holders (500 ‰ + 500 ‰), from today for five years.
 */
export async function setupRightsHarbour(
  page: Page,
  opts?: { berths?: number },
): Promise<{ marinaId: string; berthIds: string[]; rightIds: string[] }> {
  const n = opts?.berths ?? 40;
  const h = await setupOpsHarbour(page, { berths: n });
  const tag = uniq();
  const fromOn = marinaDay(0);
  const toOn = `${Number(fromOn.slice(0, 4)) + 5}${fromOn.slice(4)}`;
  // The concession: its expiry is the default end of new rights (the wizard leaves "Al" empty).
  await adminRequest(page, 'PUT', `${marinaApi(h.marinaId)}/concession`, {
    ifMatch: 0,
    body: { authority: 'Autorità di sistema portuale', expires_on: toOn },
  });
  const berthIds = h.berths.map((b) => b.id);
  const rightIds: string[] = [];
  const owned = berthIds.filter((_, i) => i % 4 === 0);
  for (const [i, berthId] of owned.entries()) {
    const first = await createCustomerWithEmail(
      page,
      h.marinaId,
      `titolare-${tag}-${i}a@e2e.porto.test`,
      `Titolare${i}A`,
    );
    const holders =
      i % 2 === 0
        ? [{ customer_id: first.id, share_millis: 1000 }]
        : [
            { customer_id: first.id, share_millis: 500 },
            {
              customer_id: (
                await createCustomerWithEmail(
                  page,
                  h.marinaId,
                  `titolare-${tag}-${i}b@e2e.porto.test`,
                  `Titolare${i}B`,
                )
              ).id,
              share_millis: 500,
            },
          ];
    const { data } = await adminRequest<{ id: string }>(
      page,
      'POST',
      `${marinaApi(h.marinaId)}/rights`,
      {
        body: {
          berth_id: berthId,
          kind: 'long_term_use',
          from_on: fromOn,
          to_on: toOn,
          title: { deed: 'Atto E2E', number: `${i + 1}` },
          holders,
        },
      },
    );
    rightIds.push(data.id);
  }
  return { marinaId: h.marinaId, berthIds, rightIds };
}
