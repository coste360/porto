import { createHmac } from 'node:crypto';
import { createServer, type IncomingHttpHeaders, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';

import { expect, test } from '@playwright/test';

import { adminPost, marinaIdOf } from '../support/api';
import { login } from '../support/session';

type Received = { headers: IncomingHttpHeaders; body: string };

/** A local receiver: the server runs with PORTO_WEBHOOKS_ALLOW_PRIVATE=true, which allows http to loopback. */
async function startReceiver(): Promise<{ server: Server; url: string; received: Received[] }> {
  const received: Received[] = [];
  const server = createServer((req, res) => {
    let body = '';
    req.setEncoding('utf8');
    req.on('data', (chunk: string) => (body += chunk));
    req.on('end', () => {
      received.push({ headers: req.headers, body });
      res.writeHead(204).end();
    });
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address() as AddressInfo;
  return { server, url: `http://127.0.0.1:${port}/porto`, received };
}

/** Checks a Porto-Signature header: "t=<unix>,v1=<hex HMAC-SHA256(secret, "<unix>.<body>")>". */
function signatureValid(header: string, secret: string, body: string): boolean {
  const parts = header.split(',');
  const t = parts.find((p) => p.startsWith('t='))?.slice(2);
  if (!t) return false;
  const want = createHmac('sha256', secret).update(`${t}.${body}`).digest('hex');
  return parts.some((p) => p === `v1=${want}`);
}

let receiver: Awaited<ReturnType<typeof startReceiver>>;

test.beforeAll(async () => {
  receiver = await startReceiver();
});

test.afterAll(async () => {
  await new Promise((resolve) => receiver.server.close(resolve));
});

test('owner creates an endpoint from the form and sees its signing secret once', async ({
  page,
}) => {
  await login(page);
  await page.getByRole('link', { name: 'Webhook' }).click();
  await page.getByRole('button', { name: 'Nuovo endpoint' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Indirizzo (https)').fill('https://hooks.example.test/porto');
  await dialog.getByLabel('Descrizione').fill('Gestionale');
  await dialog.getByLabel('member.added').check();
  await dialog.getByRole('button', { name: 'Salva' }).click();

  const secret = await dialog.getByLabel('Segreto di firma').inputValue();
  expect(secret.length).toBeGreaterThan(16);
  await dialog.getByRole('button', { name: 'Ho copiato il valore' }).click();
  await expect(page.getByRole('row', { name: /hooks\.example\.test/ })).toBeVisible();
});

test('a test event reaches a local receiver, signed, and shows as delivered', async ({ page }) => {
  test.setTimeout(60_000);
  await login(page);
  const marinaId = marinaIdOf(page);
  // The form accepts https only; the local receiver is plain http, so it is registered through the API.
  const created = await adminPost<{ endpoint: { id: string }; secret: string }>(
    page,
    `/api/admin/v1/marinas/${marinaId}/webhook-endpoints`,
    { url: receiver.url, description: 'Ricevitore locale', events: ['member.added'] },
  );

  await page.getByRole('link', { name: 'Webhook' }).click();
  const row = page.getByRole('row', { name: /Ricevitore locale/ });
  await row.getByRole('button', { name: 'Azioni' }).click();
  await page.getByRole('menuitem', { name: 'Invia un evento di prova' }).click();
  await expect(page.getByText('Evento di prova accodato.')).toBeVisible();

  await expect.poll(() => receiver.received.length, { timeout: 30_000 }).toBeGreaterThan(0);
  const hit = receiver.received[0]!;
  expect(JSON.parse(hit.body)).toMatchObject({ type: 'webhook.test' });
  expect(
    signatureValid(String(hit.headers['porto-signature'] ?? ''), created.secret, hit.body),
  ).toBe(true);

  await row.getByRole('button', { name: 'Consegne' }).click();
  await expect(async () => {
    await page.reload();
    await page
      .getByRole('row', { name: /Ricevitore locale/ })
      .getByRole('button', { name: 'Consegne' })
      .click();
    await expect(page.getByRole('cell', { name: 'Consegnato' })).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 20_000 });
});
