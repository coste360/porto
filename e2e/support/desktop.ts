import { mkdirSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

import { expect, type Page } from '@playwright/test';

/**
 * The desktop suite runs `bin/porto-desktop -headless` (no window, no browser) on its own data folder, with the
 * keyring replaced by the 0600 file store and the version check off, so it never touches the user's keychain
 * nor the network. The local network listens on 127.0.0.1 (`PORTO_DESKTOP_LAN_LOOPBACK`, dev only) so the phone
 * context can reach it on any CI machine.
 */
function runDir(): string {
  if (!process.env.PORTO_E2E_DESKTOP_DIR) {
    process.env.PORTO_E2E_DESKTOP_DIR = mkdtempSync(path.join(tmpdir(), 'porto-e2e-desktop-'));
  }
  return process.env.PORTO_E2E_DESKTOP_DIR;
}

export const DESKTOP_RUN_DIR = runDir();
export const DESKTOP_DATA_DIR = path.join(DESKTOP_RUN_DIR, 'data');
mkdirSync(DESKTOP_DATA_DIR, { recursive: true });

export const DESKTOP_PORT = Number(process.env.PORTO_E2E_DESKTOP_PORT ?? 18090);
export const DESKTOP_LAN_PORT = Number(process.env.PORTO_E2E_DESKTOP_LAN_PORT ?? 18443);
export const DESKTOP_URL = `http://127.0.0.1:${DESKTOP_PORT}`;
export const DESKTOP_BINARY = path.resolve(
  import.meta.dirname,
  '..',
  '..',
  'bin',
  process.platform === 'win32' ? 'porto-desktop.exe' : 'porto-desktop',
);

/** The owner created by the first-run spec; the later specs sign in with it. */
export const DESKTOP_OWNER = {
  org: 'Porto Desktop SRL',
  name: 'Delia Desktop',
  email: 'owner@desktop.test',
  password: 'e2e-desktop-password-123',
  marina: 'Marina Desktop',
  slug: 'desktop',
} as const;

export function desktopEnv(): Record<string, string> {
  return {
    PORTO_DEV: 'true',
    PORTO_DESKTOP_KEYRING: 'file',
    PORTO_DESKTOP_PORT: String(DESKTOP_PORT),
    PORTO_DESKTOP_LAN_PORT: String(DESKTOP_LAN_PORT),
    PORTO_DESKTOP_LAN_LOOPBACK: 'true',
    PORTO_UPDATE_CHECK: 'false',
    PORTO_DESKTOP_FIRST_RUN_FILE: 'true',
    PORTO_LOG_LEVEL: 'warn',
  };
}

/**
 * The first-run URL the launcher wrote for the tests (`<data>/first-run.url`, deleted once used). The token is
 * in the fragment; the path is turned relative to the suite base URL.
 */
export function firstRunUrl(): string | null {
  try {
    const raw = readFileSync(path.join(DESKTOP_DATA_DIR, 'first-run.url'), 'utf8').trim();
    const url = new URL(raw);
    return url.pathname + url.hash;
  } catch {
    return null;
  }
}

/** Signs the owner in through the API (the page context gets the session cookies) and opens the panel. */
export async function signInOwner(page: Page): Promise<void> {
  const res = await page.request.post('/api/admin/v1/auth/login', {
    data: { email: DESKTOP_OWNER.email, password: DESKTOP_OWNER.password },
  });
  expect(res.ok(), `login: ${res.status()}`).toBeTruthy();
  await page.goto('/admin/');
  await expect(page).toHaveURL(/\/admin\/m\/[^/]+/);
}

/** CSRF header of the page's cookie session (double submit). */
export async function csrfHeaders(page: Page): Promise<Record<string, string>> {
  const cookies = await page.context().cookies();
  return {
    'X-CSRF-Token': cookies.find((c) => c.name === 'porto_csrf')?.value ?? '',
  };
}

/** Switches the local network through the desktop API (owner, loopback). */
export async function setLan(page: Page, enabled: boolean): Promise<void> {
  const res = await page.request.put('/api/desktop/v1/lan', {
    data: { enabled },
    headers: await csrfHeaders(page),
  });
  expect(res.ok(), `lan: ${res.status()} ${await res.text()}`).toBeTruthy();
}

/** Adds a dock member with a unique name to the marina through the admin API; returns the name. */
export async function addDockMember(page: Page, marinaId: string): Promise<string> {
  const stamp = Date.now();
  const name = `Dario Banchina ${stamp}`;
  const res = await page.request.post(`/api/admin/v1/marinas/${marinaId}/members`, {
    data: { name, email: `dock-${stamp}@desktop.test`, role: 'dock', locale: 'it' },
    headers: { ...(await csrfHeaders(page)), 'Idempotency-Key': crypto.randomUUID() },
  });
  expect(res.ok(), `member: ${res.status()} ${await res.text()}`).toBeTruthy();
  return name;
}

/** Global teardown: removes the run's data folder; set PORTO_E2E_KEEP=1 to inspect it after a run. */
export default function globalTeardown() {
  if (process.env.PORTO_E2E_KEEP) {
    console.log(`porto e2e desktop: data kept in ${DESKTOP_RUN_DIR}`);
    return;
  }
  rmSync(DESKTOP_RUN_DIR, { recursive: true, force: true });
}
