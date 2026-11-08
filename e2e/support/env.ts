import { mkdirSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

/**
 * One scratch directory per run, shared by the runner and its workers through the environment
 * (the config is evaluated in every worker; only the first evaluation creates the directory).
 */
function runDir(): string {
  if (!process.env.PORTO_E2E_DIR) {
    process.env.PORTO_E2E_DIR = mkdtempSync(path.join(tmpdir(), 'porto-e2e-'));
  }
  return process.env.PORTO_E2E_DIR;
}

export const RUN_DIR = runDir();
export const DATA_DIR = path.join(RUN_DIR, 'data');
export const MAIL_DIR = path.join(RUN_DIR, 'mail');
mkdirSync(DATA_DIR, { recursive: true });
mkdirSync(MAIL_DIR, { recursive: true });

export const PORT = Number(process.env.PORTO_E2E_PORT ?? 18080);
export const BASE_URL = `http://127.0.0.1:${PORT}`;
// M8 (m8-plan §14.5): the embedding host page (another origin) and the Stripe API stub.
export const HOST_PORT = Number(process.env.PORTO_E2E_HOST_PORT ?? 18081);
export const HOST_URL = `http://localhost:${HOST_PORT}`;
export const STRIPE_STUB_PORT = Number(process.env.PORTO_E2E_STRIPE_PORT ?? 18082);
export const STRIPE_STUB_URL = `http://127.0.0.1:${STRIPE_STUB_PORT}`;
export const BINARY = path.resolve(import.meta.dirname, '..', '..', 'bin', 'porto');

/** The bootstrap owner created before the server starts (see playwright.config.ts). */
export const OWNER = {
  email: 'owner@porto.test',
  name: 'Olga Ormeggi',
  password: 'e2e-owner-password-123',
  org: 'Porto E2E SRL',
  marina: 'Marina E2E',
  slug: 'e2e',
} as const;

/** Environment of every porto process started by the suite. */
export function portoEnv(): Record<string, string> {
  return {
    PORTO_DEV: 'true',
    PORTO_DATA_DIR: DATA_DIR,
    PORTO_MAIL_DIR: MAIL_DIR,
    PORTO_ADDR: `127.0.0.1:${PORT}`,
    PORTO_BASE_URL: BASE_URL,
    PORTO_AUTO_MIGRATE: 'true',
    PORTO_LOG_LEVEL: 'warn',
    PORTO_WEBHOOKS_ALLOW_PRIVATE: 'true',
    PORTO_STRIPE_API_BASE: STRIPE_STUB_URL,
  };
}
