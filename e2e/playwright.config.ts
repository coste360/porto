import { defineConfig, devices } from '@playwright/test';

import { BASE_URL, BINARY, OWNER, portoEnv } from './support/env';

// The server runs the binary built by `make build` (make e2e builds it first) on an empty data directory.
// The bootstrap owner is created by the CLI before `serve` starts, so the suite never races the server for
// the SQLite file and exercises `porto admin create` as an operator would.
const adminCreate = [
  `"${BINARY}" admin create`,
  `--email ${OWNER.email}`,
  `--name "${OWNER.name}"`,
  `--org "${OWNER.org}"`,
  `--marina "${OWNER.marina}"`,
  `--slug ${OWNER.slug}`,
].join(' ');

export default defineConfig({
  testDir: './tests',
  globalTeardown: './support/teardown.ts',
  // One server, one database: specs share state (owner, marina), so they run one at a time.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  timeout: 30_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: BASE_URL,
    locale: 'it-IT',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: `${adminCreate} && exec "${BINARY}" serve`,
    url: `${BASE_URL}/readyz`,
    env: { ...portoEnv(), PORTO_ADMIN_PASSWORD: OWNER.password },
    reuseExistingServer: false,
    timeout: 60_000,
    stdout: 'ignore',
    stderr: 'pipe',
  },
});
