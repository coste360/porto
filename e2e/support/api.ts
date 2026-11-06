import { expect, type Page } from '@playwright/test';

/** The marina id of the shell the page is on (/admin/m/<id>/…). */
export function marinaIdOf(page: Page): string {
  const m = new URL(page.url()).pathname.match(/\/admin\/m\/([^/]+)/);
  if (!m) throw new Error(`not in a marina shell: ${page.url()}`);
  return m[1]!;
}

/**
 * Calls the admin API with the page's session, as the SPA does: the porto_csrf cookie is echoed in
 * X-CSRF-Token (double submit) and unsafe calls carry an Idempotency-Key.
 */
export async function adminPost<T>(page: Page, path: string, body: unknown): Promise<T> {
  const cookies = await page.context().cookies();
  const csrf = cookies.find((c) => c.name === 'porto_csrf')?.value ?? '';
  const res = await page.request.post(path, {
    data: body,
    headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': crypto.randomUUID() },
  });
  expect(res.ok(), `${path}: ${res.status()} ${await res.text()}`).toBeTruthy();
  return (await res.json()) as T;
}
