import { expect, request, test } from '@playwright/test';

import { DESKTOP_LAN_PORT, setLan, signInOwner } from '../support/desktop';

// M6b local CA for phones (m6b-plan §8.5): a name-constrained CA (created by this run's first start) is offered on
// the local network listener as a DER certificate and as an iOS profile; the settings page links it and the help
// page.

test('the CA certificate and the iOS profile are downloadable on the local network', async ({ page }) => {
  await signInOwner(page);
  await setLan(page, true);
  const lan = await (await page.request.get('/api/desktop/v1/lan')).json();
  expect(lan.ca_constrained).toBe(true);
  expect(lan.ca_download_url).toBe(`${lan.url}/lan/porto-ca.crt`);

  // A phone on the Wi-Fi: no session, the self-signed leaf accepted.
  const phone = await request.newContext({ ignoreHTTPSErrors: true });
  const base = `https://127.0.0.1:${DESKTOP_LAN_PORT}`;
  const crt = await phone.get(`${base}/lan/porto-ca.crt`);
  expect(crt.status()).toBe(200);
  expect(crt.headers()['content-type']).toBe('application/x-x509-ca-cert');
  expect((await crt.body()).length).toBeGreaterThan(200);
  const profile = await phone.get(`${base}/lan/porto-ca.mobileconfig`);
  expect(profile.status()).toBe(200);
  expect(await profile.text()).toContain('com.apple.security.root');
  // The bridge and the desktop API stay unreachable from the local network.
  expect((await phone.get(`${base}/api/bridge/v1/status`)).status()).toBe(404);
  await phone.dispose();

  await page.goto('/admin/desktop');
  const link = page.getByTestId('lan-ca-download');
  await expect(link.getByRole('link', { name: 'Scarica il certificato per i telefoni' })).toHaveAttribute(
    'href',
    lan.ca_download_url,
  );
  await expect(link.getByRole('link', { name: 'Come installarlo' })).toHaveAttribute(
    'href',
    '/admin/help/certificate',
  );
  await setLan(page, false);
});
