import { expect, test } from '@playwright/test';

import { login } from '../support/session';
import {
  createCustomer,
  createDocument,
  createVessel,
  dayFromToday,
  shownDate,
  uniq,
} from '../support/registry';

test('an expiring insurance shows as due soon and can be marked done', async ({ page }) => {
  const tag = uniq();
  const due = dayFromToday(10);
  await login(page);
  const owner = await createCustomer(page, {
    kind: 'person',
    first_name: 'Dario',
    last_name: `Scad${tag}`,
  });
  const vessel = await createVessel(page, {
    name: `Tramontana ${tag}`,
    loa_cm: 900,
    beam_cm: 320,
    parties: [{ customer_id: owner.id, role: 'owner', share_millis: 1000 }],
  });
  await createDocument(page, {
    owner_type: 'vessel',
    vessel_id: vessel.id,
    type: 'insurance_policy',
    expires_on: due,
  });

  // The dashboard card counts it among the deadlines due soon.
  await page.reload();
  await expect(page.getByTestId('deadlines-due_soon')).not.toHaveText('0');

  await page.getByRole('link', { name: 'Scadenze', exact: true }).first().click();
  const section = page.getByRole('region', { name: /In scadenza/ });
  const row = section.getByRole('row', {
    name: new RegExp(`Tramontana ${tag}`),
  });
  await expect(row).toBeVisible();
  await expect(row.getByText(shownDate(due))).toBeVisible();
  await row.getByRole('button', { name: 'Gestita' }).click();
  await expect(page.getByText('Scadenza aggiornata.')).toBeVisible();
  await expect(section.getByRole('row', { name: new RegExp(`Tramontana ${tag}`) })).toHaveCount(0);
});
