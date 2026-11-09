import { createHash, createHmac } from 'node:crypto';

import { expect, type APIRequestContext } from '@playwright/test';

// Helpers of the M12 sensor specs (m12-plan §17.4): a device created through the admin API and readings signed as
// docs/sensor-protocol.md §4.2 describes (HMAC-SHA256 of "v1\nt\nPOST\npath\nsha256(body)" with the full key).

export const INGEST_PATH = '/api/ingest/v1/readings';

/** Posts readings signed with a device key (a request context without the session cookies, as a device). */
export async function postSignedReadings(
  request: APIRequestContext,
  key: string,
  readings: { device_id: string; observed_at: string; state: string }[],
): Promise<{ accepted: number; duplicates: number }> {
  const body = JSON.stringify({ readings });
  const t = Math.floor(Date.now() / 1000);
  const hash = createHash('sha256').update(body).digest('hex');
  const sig = createHmac('sha256', key).update(`v1\n${t}\nPOST\n${INGEST_PATH}\n${hash}`).digest('hex');
  const keyId = key.split('_').slice(0, 3).join('_');
  const res = await request.post(INGEST_PATH, {
    data: body,
    headers: { 'Content-Type': 'application/json', 'Porto-Key': keyId, 'Porto-Signature': `t=${t},v1=${sig}` },
  });
  expect(res.ok(), `ingest: ${res.status()} ${await res.text()}`).toBeTruthy();
  return (await res.json()) as { accepted: number; duplicates: number };
}

/** A pair of agreeing readings (confirms the state with the default hysteresis of 2 readings). */
export function readingPair(externalId: string, state: string) {
  const now = Date.now();
  return [
    { device_id: externalId, observed_at: new Date(now - 60_000).toISOString(), state },
    { device_id: externalId, observed_at: new Date(now).toISOString(), state },
  ];
}
