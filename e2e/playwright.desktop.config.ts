import { defineConfig } from '@playwright/test';

import { DESKTOP_BINARY, DESKTOP_DATA_DIR, DESKTOP_URL, desktopEnv } from './support/desktop';

// Desktop edition suite (m6a-plan §10.4): the headless launcher (`make e2e-desktop` builds it without CGO) on an
// empty data folder. The first-run spec creates the owner and the marina once (setup project); the other specs
// sign in with that owner. The phone of the pairing spec is a `devices['Pixel 7']` context created inside it.
export default defineConfig({
  testDir: './tests-desktop',
  globalTeardown: './support/desktop.ts',
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  timeout: 90_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: DESKTOP_URL,
    locale: 'it-IT',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'first-run',
      testMatch: /first-run\.spec\.ts/,
      use: { browserName: 'chromium' },
    },
    {
      name: 'chromium',
      testIgnore: /first-run\.spec\.ts/,
      dependencies: ['first-run'],
      use: { browserName: 'chromium', viewport: { width: 1280, height: 800 } },
    },
  ],
  webServer: {
    command: `exec "${DESKTOP_BINARY}" -headless -data-dir "${DESKTOP_DATA_DIR}"`,
    url: `${DESKTOP_URL}/readyz`,
    env: desktopEnv(),
    reuseExistingServer: false,
    timeout: 60_000,
    stdout: 'ignore',
    stderr: 'pipe',
  },
});
