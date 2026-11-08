import { defineConfig, devices } from "@playwright/test";

import {
  BASE_URL,
  BINARY,
  HOST_PORT,
  HOST_URL,
  OWNER,
  STRIPE_STUB_PORT,
  STRIPE_STUB_URL,
  portoEnv,
} from "./support/env";

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
].join(" ");

/** Specs of the phone project (M6b): phone-*.spec.ts, offline-*.spec.ts and pwa.spec.ts. */
const PHONE_SPECS = /(phone|offline)-.*\.spec\.ts|pwa\.spec\.ts/;

export default defineConfig({
  testDir: "./tests",
  globalTeardown: "./support/teardown.ts",
  // One server, one database: specs share state (owner, marina), so they run one at a time.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  timeout: 30_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: BASE_URL,
    locale: "it-IT",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "chromium",
      testIgnore: PHONE_SPECS,
      use: { ...devices["Desktop Chrome"] },
    },
    // M6b phone screens, offline queue and PWA (m6b-plan §10): a Pixel 7 with service workers allowed.
    {
      name: "phone",
      testMatch: PHONE_SPECS,
      use: { ...devices["Pixel 7"], serviceWorkers: "allow" },
    },
  ],
  webServer: [
    // M8 (m8-plan §14.5): the Stripe API stub (PORTO_STRIPE_API_BASE) and the embedding host page on another
    // origin; both start before Porto.
    {
      command: "node support/stripe-stub.mjs",
      url: `${STRIPE_STUB_URL}/healthz`,
      env: { PORTO_E2E_STRIPE_PORT: String(STRIPE_STUB_PORT) },
      reuseExistingServer: false,
      timeout: 15_000,
      stdout: "ignore",
      stderr: "pipe",
    },
    {
      command: "node support/host-server.mjs",
      url: `${HOST_URL}/healthz`,
      env: { PORTO_E2E_HOST_PORT: String(HOST_PORT), PORTO_E2E_BASE_URL: BASE_URL, PORTO_E2E_SLUG: OWNER.slug },
      reuseExistingServer: false,
      timeout: 15_000,
      stdout: "ignore",
      stderr: "pipe",
    },
    {
      command: `${adminCreate} && exec "${BINARY}" serve`,
      url: `${BASE_URL}/readyz`,
      env: { ...portoEnv(), PORTO_ADMIN_PASSWORD: OWNER.password },
      reuseExistingServer: false,
      timeout: 60_000,
      stdout: "ignore",
      stderr: "pipe",
    },
  ],
});
