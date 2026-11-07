import type { Page } from '@playwright/test';

import { adminRequest, marinaApi } from './map';
import { createVesselViaApi, marinaDay, setupOpsHarbour, setupPrices } from './occupancy';

/** A vessel that fits the 4 m × 12 m berths of setupOpsHarbour (10,00 m × 3,40 m). */
export const BERTH_VESSEL = { loaCm: 1000, beamCm: 340 };

// M4 contract helpers for the Playwright specs (m4-plan §9.5; signatures fixed). Everything goes through the
// admin API with the page session: a fresh marina per scenario (M3 setupOpsHarbour), synthetic customers with a
// `*@e2e.porto.test` email (the dev mail directory receives every message), synthetic vessels.

const uniq = () => Date.now().toString(36).slice(-5) + Math.floor(Math.random() * 1e4).toString(36);

/** The email of the n-th synthetic customer of a scenario (the mail helpers read it back). */
export function customerEmail(tag: string, n: number): string {
  return `cliente-${tag}-${n}@e2e.porto.test`;
}

/** A synthetic person customer with an email, the marketing consent unset. */
export async function createCustomerWithEmail(
  page: Page,
  marinaId: string,
  email: string,
  lastName = `M4${uniq()}`,
): Promise<{ id: string; display_name: string; email: string }> {
  const { data } = await adminRequest<{
    customer: { id: string; display_name: string };
  }>(page, 'POST', `${marinaApi(marinaId)}/customers`, {
    body: {
      kind: 'person',
      first_name: 'Cliente',
      last_name: lastName,
      emails: [{ label: 'principale', value: email }],
    },
  });
  return { ...data.customer, email };
}

/** Contract settings with the OTP driver and bank details (deposit instructions on the public pages). */
export async function useOtpSignature(page: Page, marinaId: string): Promise<void> {
  const api = marinaApi(marinaId);
  const current = await adminRequest<{ version: number }>(page, 'GET', `${api}/contract-settings`);
  await adminRequest(page, 'PUT', `${api}/contract-settings`, {
    ifMatch: current.data.version,
    body: {
      signature_driver: 'otp',
      require_paper_scan: true,
      bank_details: {
        iban: 'IT60X0542811101000000123456',
        holder: 'Marina E2E',
        bank: 'Banca E2E',
      },
    },
  });
}

/**
 * Creates a marina with `contracts` (default 20) active annual contracts ready for a renewal campaign: one berth
 * per contract, the contracts started one year ago minus 20 days (so they end within the next month), prices for
 * this and next year, the OTP signature driver.
 */
export async function setupRenewalScenario(
  page: Page,
  opts?: { contracts?: number },
): Promise<{ marinaId: string; contractIds: string[] }> {
  const n = opts?.contracts ?? 20;
  // Two berths more than the contracts: specs quote or move onto a free berth.
  const h = await setupOpsHarbour(page, { berths: n + 2 });
  await setupPrices(page, h.marinaId);
  await useOtpSignature(page, h.marinaId);
  const tag = uniq();
  const startsOn = marinaDay(-345);
  const [y, m, d] = startsOn.split('-').map(Number) as [number, number, number];
  const endsOn = new Date(Date.UTC(y + 1, m - 1, Math.min(d, m === 2 ? 28 : d)))
    .toISOString()
    .slice(0, 10);
  const lastNight = new Date(Date.parse(`${endsOn}T00:00:00Z`) - 86_400_000)
    .toISOString()
    .slice(0, 10);
  const contractIds: string[] = [];
  // Ten requests at a time: the SQLite write queue keeps them ordered anyway.
  for (let start = 0; start < n; start += 10) {
    const batch = await Promise.all(
      h.berths.slice(start, Math.min(n, start + 10)).map(async (b, i) => {
        const customer = await createCustomerWithEmail(
          page,
          h.marinaId,
          customerEmail(tag, start + i + 1),
        );
        const vessel = await createVesselViaApi(page, h.marinaId, BERTH_VESSEL);
        // A valid insurance policy: a renewal campaign excludes vessels uninsured on the new start (11.5).
        await adminRequest(page, 'POST', `${marinaApi(h.marinaId)}/documents`, {
          body: {
            owner_type: 'vessel',
            vessel_id: vessel.id,
            type: 'insurance_policy',
            expires_on: `${Number(marinaDay().slice(0, 4)) + 3}-12-31`,
          },
        });
        const { data } = await adminRequest<{ id: string }>(
          page,
          'POST',
          `${marinaApi(h.marinaId)}/contracts`,
          {
            body: {
              term: 'annual',
              customer_id: customer.id,
              vessel_id: vessel.id,
              starts_on: startsOn,
              ends_on: endsOn,
              placements: [
                {
                  resource_type: 'berth',
                  resource_id: b.id,
                  first_night: startsOn,
                  last_night: lastNight,
                },
              ],
              activate: true,
            },
          },
        );
        return data.id;
      }),
    );
    contractIds.push(...batch);
  }
  return { marinaId: h.marinaId, contractIds };
}

/** The customer email of a contract (scenario helpers create one customer per contract). */
export async function contractCustomerEmail(
  page: Page,
  marinaId: string,
  contractId: string,
): Promise<string> {
  const c = await adminRequest<{ customer?: { id: string } }>(
    page,
    'GET',
    `${marinaApi(marinaId)}/contracts/${contractId}`,
  );
  const cust = await adminRequest<{ emails?: { value: string }[] }>(
    page,
    'GET',
    `${marinaApi(marinaId)}/customers/${c.data.customer!.id}`,
  );
  return cust.data.emails?.[0]?.value ?? '';
}

/** A mooring quote with one held option on a berth for a year from `startsOn`. */
export async function createQuoteViaApi(
  page: Page,
  marinaId: string,
  o: {
    customerId: string;
    vesselId: string;
    berthId: string;
    startsOn: string;
  },
): Promise<{ id: string; version: number; code: string }> {
  const [y] = o.startsOn.split('-').map(Number) as [number];
  const endsOn = `${y + 1}${o.startsOn.slice(4)}`;
  const lastNight = new Date(Date.parse(`${endsOn}T00:00:00Z`) - 86_400_000)
    .toISOString()
    .slice(0, 10);
  const { data } = await adminRequest<{
    id: string;
    version: number;
    code: string;
  }>(page, 'POST', `${marinaApi(marinaId)}/quotes`, {
    body: {
      kind: 'mooring',
      term: 'annual',
      customer_id: o.customerId,
      vessel_id: o.vesselId,
      starts_on: o.startsOn,
      ends_on: endsOn,
      options: [
        {
          hold: true,
          placements: [
            {
              resource_type: 'berth',
              resource_id: o.berthId,
              first_night: o.startsOn,
              last_night: lastNight,
            },
          ],
        },
      ],
    },
  });
  return data;
}

/** A draft annual contract (not activated) for a fresh customer with an email, for the signature specs. */
export async function createDraftContract(
  page: Page,
  marinaId: string,
  berthId: string,
  email: string,
): Promise<{ id: string; customerId: string }> {
  const customer = await createCustomerWithEmail(page, marinaId, email);
  const vessel = await createVesselViaApi(page, marinaId, BERTH_VESSEL);
  const startsOn = marinaDay(1);
  const [y] = startsOn.split('-').map(Number) as [number];
  const endsOn = `${y + 1}${startsOn.slice(4)}`;
  const lastNight = new Date(Date.parse(`${endsOn}T00:00:00Z`) - 86_400_000)
    .toISOString()
    .slice(0, 10);
  const { data } = await adminRequest<{ id: string }>(
    page,
    'POST',
    `${marinaApi(marinaId)}/contracts`,
    {
      body: {
        term: 'annual',
        customer_id: customer.id,
        vessel_id: vessel.id,
        starts_on: startsOn,
        ends_on: endsOn,
        placements: [
          {
            resource_type: 'berth',
            resource_id: berthId,
            first_night: startsOn,
            last_night: lastNight,
          },
        ],
        activate: false,
      },
    },
  );
  return { id: data.id, customerId: customer.id };
}

export { uniq as uniqTag };
