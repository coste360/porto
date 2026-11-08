import { expect, test } from '@playwright/test';

import { billingRequest, createBillingItem, openCashSession } from '../support/billing';
import {
  APP_PIN,
  fakePeripheralLines,
  firstRunUrlOf,
  onlineOwner,
  remoteEnterUrl,
  startSecondDesktop,
  type SecondDesktop,
} from '../support/desktop';

// M6b remote mode (m6b-plan §7, §10.3): a second desktop app on its first run pairs to the online Porto
// (`bin/porto serve`, http allowed in development) through "Collega a un Porto online", restarts in remote mode on
// the same port, serves the online panel through its proxy with the device token, prints a courtesy receipt on
// its fake printer, keeps the cached shell when the instance is unreachable and unpairs with the app PIN.

test.describe.configure({ mode: 'serial' });

let app: SecondDesktop;
let enter = '';
let paymentId = '';
let marinaId = '';

test.beforeAll(async () => {
  app = await startSecondDesktop();
});

test.afterAll(async () => {
  await app?.stop();
});

const pinHeaders = (origin: string) => ({ 'Porto-App-Pin': APP_PIN, Origin: origin });

// The launch secret of the enter URL is single use: the first test exchanges it for the porto_remote cookie, the
// later ones (new browser contexts) reuse that cookie.
let proxyCookies: Awaited<ReturnType<import('@playwright/test').BrowserContext['cookies']>> = [];

async function openProxy(page: import('@playwright/test').Page): Promise<string> {
  const origin = new URL(enter).origin;
  if (proxyCookies.length === 0) {
    await page.goto(enter);
    await expect(page).toHaveURL(/\/admin\//);
    proxyCookies = await page.context().cookies(origin);
  } else {
    await page.context().addCookies(proxyCookies);
    await page.goto(`${origin}/admin/`);
  }
  return origin;
}

test('first run → "Collega a un Porto online" with the copied link and an app PIN', async ({
  page,
}) => {
  const online = await onlineOwner();
  marinaId = online.marinaId;
  // A receipt-mode counter sale on the online instance, to print from the desktop later.
  const item = await createBillingItem(online.api, marinaId);
  await openCashSession(online.api, marinaId, 0);
  const sale = await billingRequest<{ payment: { id: string } }>(
    online.api,
    'POST',
    `/api/admin/v1/marinas/${marinaId}/counter-sales`,
    {
      body: {
        lines: [{ item_id: item.id, qty_milli: 1000, unit_cents: 1500 }],
        payment: { method: 'cash' },
        fiscal_document: true,
        receipt: 'none',
      },
    },
  );
  paymentId = sale.payment.id;
  const code = await billingRequest<{ qr_payload: string }>(
    online.api,
    'POST',
    `/api/admin/v1/marinas/${marinaId}/devices/pairing-codes`,
    { body: { kind: 'desktop' } },
  );
  expect(code.qr_payload).toMatch(/^porto:\/\/pair\?v=1&url=/);

  await page.goto(firstRunUrlOf(app.dataDir));
  await page.getByTestId('first-run-remote').click();
  const form = page.getByTestId('remote-pair-page');
  await form.getByLabel('Collegamento').fill(code.qr_payload);
  await form.getByLabel("PIN dell'app").fill(APP_PIN);
  await form.getByLabel('Ripeti il PIN').fill(APP_PIN);
  await form.getByRole('button', { name: 'Collega' }).click();
  await expect(page.getByTestId('remote-pair-done')).toBeVisible();

  enter = await remoteEnterUrl(app.dataDir);
  expect(enter).toContain('/__porto/enter#k=');
});

test('the proxy serves the online instance with the device token and the local bridge', async ({
  page,
}) => {
  const origin = await openProxy(page);
  const status = await (await page.request.get(`${origin}/api/desktop/v1/status`)).json();
  expect(status.mode).toBe('remote');
  expect(status.remote.reachable).toBe(true);

  const orgs = await page.request.get(`${origin}/api/admin/v1/organizations`);
  expect(orgs.status()).toBe(200);
  expect(await orgs.text()).not.toContain('po_dev_');

  // Fake peripherals (app PIN), then "Stampa scontrino" through the bridge.
  const set = await page.request.put(`${origin}/api/desktop/v1/peripherals`, {
    headers: pinHeaders(origin),
    data: {
      receipt: { driver: 'fake', width: 80 },
      label: { driver: 'none' },
      reader: { driver: 'keyboard' },
      fiscal: { driver: 'fake', fake_mode: 'ok' },
    },
  });
  expect(set.status(), await set.text()).toBe(200);
  const bridge = await (await page.request.get(`${origin}/api/bridge/v1/status`)).json();
  expect(bridge.receipt).toMatchObject({ driver: 'fake', ready: true });
  const print = await page.request.post(`${origin}/api/bridge/v1/receipts`, {
    headers: { Origin: origin },
    data: { payment_id: paymentId },
  });
  expect(print.status(), await print.text()).toBe(202);
  expect(fakePeripheralLines(app.dataDir, 'receipt')).toHaveLength(1);
});

test('the panel of the online instance loads through the proxy', async ({ page }) => {
  // getMe accepts the device token the proxy adds (deviceAuth): the panel starts without a cookie session.
  await openProxy(page);
  await expect(page).toHaveURL(new RegExp(`/admin/m/${marinaId}`));
});

test('unreachable instance: cached shell, API 503 remote_unreachable, then back online', async ({
  page,
}) => {
  const origin = await openProxy(page);
  const off = await page.request.post(`${origin}/api/desktop/v1/dev/remote-offline`, {
    headers: { Origin: origin },
    data: { offline: true },
  });
  expect(off.status()).toBe(204);
  const shell = await page.reload();
  expect(shell?.status()).toBe(200);
  const api = await page.request.get(`${origin}/api/admin/v1/organizations`);
  expect(api.status()).toBe(503);
  expect((await api.json()).type).toContain('remote_unreachable');
  const status = await (await page.request.get(`${origin}/api/desktop/v1/status`)).json();
  expect(status.remote.reachable).toBe(false);
  await page.request.post(`${origin}/api/desktop/v1/dev/remote-offline`, {
    headers: { Origin: origin },
    data: { offline: false },
  });
  expect((await page.request.get(`${origin}/api/admin/v1/organizations`)).status()).toBe(200);
});

test('unpair with the app PIN: the device is revoked and the app is back on the first run', async ({
  page,
}) => {
  const origin = await openProxy(page);
  const wrong = await page.request.delete(`${origin}/api/desktop/v1/remote`, {
    headers: { 'Porto-App-Pin': '000000', Origin: origin },
  });
  expect(wrong.status()).toBe(401);
  const ok = await page.request.delete(`${origin}/api/desktop/v1/remote`, {
    headers: pinHeaders(origin),
  });
  expect(ok.status()).toBe(202);
  await expect
    .poll(async () => {
      const r = await page.request.get(`${origin}/api/desktop/v1/status`).catch(() => null);
      return r?.ok() ? (await r.json()).mode : null;
    })
    .toBe('local');
  const online = await onlineOwner();
  const devices = await (
    await online.api.get(`/api/admin/v1/marinas/${online.marinaId}/devices`)
  ).json();
  const desktops = (devices.items as { kind: string; revoked_at: string | null }[]).filter(
    (d) => d.kind === 'desktop',
  );
  expect(desktops.length).toBeGreaterThan(0);
  expect(desktops.every((d) => d.revoked_at)).toBe(true);
});
