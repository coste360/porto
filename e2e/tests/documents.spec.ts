import path from 'node:path';

import { expect, test } from '@playwright/test';

import { login } from '../support/session';
import {
  createCustomer,
  createDocument,
  createVessel,
  dayFromToday,
  uniq,
} from '../support/registry';

const SAMPLE_PDF = path.resolve(import.meta.dirname, '..', 'fixtures', 'sample.pdf');

test('uploads a PDF and views it through the signed link', async ({ page }) => {
  const tag = uniq();
  await login(page);
  const owner = await createCustomer(page, {
    kind: 'person',
    first_name: 'Carla',
    last_name: `Doc${tag}`,
  });
  const vessel = await createVessel(page, {
    name: `Grecale ${tag}`,
    loa_cm: 1000,
    beam_cm: 350,
    parties: [{ customer_id: owner.id, role: 'owner', share_millis: 1000 }],
  });
  await createDocument(page, {
    owner_type: 'vessel',
    vessel_id: vessel.id,
    type: 'insurance_policy',
    number: `POL-${tag}`,
    expires_on: dayFromToday(10),
  });

  const shell = new URL(page.url()).pathname.replace(/\/$/, '');
  await page.goto(`${shell}/vessels/${vessel.id}`);
  await page.getByRole('tab', { name: /Documenti/ }).click();
  await expect(page.getByText('Polizza assicurativa')).toBeVisible();

  const chooser = page.waitForEvent('filechooser');
  await page.getByRole('button', { name: 'Carica file' }).click();
  await (await chooser).setFiles(SAMPLE_PDF);
  await expect(page.getByText('File caricato.')).toBeVisible();

  // The file opens in a new tab through the signed link (a headless browser may download the PDF instead
  // of showing it): catch the signed request, then fetch the same link with the session.
  const popup = page.waitForEvent('popup');
  const signed = page
    .context()
    .waitForEvent('request', (r) => /\/documents\/[^/]+\/file\?exp=\d+&sig=/.test(r.url()));
  await page.getByRole('button', { name: 'sample.pdf' }).click();
  const tab = await popup;
  const url = (await signed).url();
  const response = await page.request.get(url);
  expect(response.status()).toBe(200);
  expect(response.headers()['content-type']).toContain('application/pdf');
  expect(response.headers()['cache-control']).toContain('no-store');
  expect((await response.body()).subarray(0, 5).toString()).toBe('%PDF-');
  await tab.close();
});
