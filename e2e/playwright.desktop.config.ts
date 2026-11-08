import { mkdirSync } from "node:fs";
import path from "node:path";

import { defineConfig } from "@playwright/test";

import {
  DESKTOP_BINARY,
  DESKTOP_DATA_DIR,
  DESKTOP_RUN_DIR,
  DESKTOP_URL,
  desktopEnv,
} from "./support/desktop";
import { BINARY, OWNER } from "./support/env";

// M6b remote mode (m6b-plan §10.3): an online Porto (`bin/porto serve`) the desktop pairs to, on its own port and
// data folder, with the same bootstrap owner as the server suite.
const REMOTE_PORT = Number(process.env.PORTO_E2E_REMOTE_PORT ?? 18100);
const REMOTE_URL = `http://127.0.0.1:${REMOTE_PORT}`;
const REMOTE_DATA_DIR = path.join(DESKTOP_RUN_DIR, "remote-data");
const REMOTE_MAIL_DIR = path.join(DESKTOP_RUN_DIR, "remote-mail");
mkdirSync(REMOTE_DATA_DIR, { recursive: true });
mkdirSync(REMOTE_MAIL_DIR, { recursive: true });
const remoteAdminCreate = [
  `"${BINARY}" admin create`,
  `--email ${OWNER.email}`,
  `--name "${OWNER.name}"`,
  `--org "${OWNER.org}"`,
  `--marina "${OWNER.marina}"`,
  `--slug ${OWNER.slug}`,
].join(" ");

// Desktop edition suite (m6a-plan §10.4): the headless launcher (`make e2e-desktop` builds it without CGO) on an
// empty data folder. The first-run spec creates the owner and the marina once (setup project); the other specs
// sign in with that owner. The phone of the pairing spec is a `devices['Pixel 7']` context created inside it.
export default defineConfig({
  testDir: "./tests-desktop",
  globalTeardown: "./support/desktop.ts",
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  timeout: 90_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: DESKTOP_URL,
    locale: "it-IT",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "first-run",
      testMatch: /first-run\.spec\.ts/,
      use: { browserName: "chromium" },
    },
    {
      name: "chromium",
      testIgnore: /first-run\.spec\.ts/,
      dependencies: ["first-run"],
      use: { browserName: "chromium", viewport: { width: 1280, height: 800 } },
    },
  ],
  webServer: [
    {
      command: `exec "${DESKTOP_BINARY}" -headless -data-dir "${DESKTOP_DATA_DIR}"`,
      url: `${DESKTOP_URL}/readyz`,
      env: desktopEnv(),
      reuseExistingServer: false,
      timeout: 60_000,
      stdout: "ignore",
      stderr: "pipe",
    },
    {
      command: `${remoteAdminCreate} && exec "${BINARY}" serve`,
      url: `${REMOTE_URL}/readyz`,
      env: {
        PORTO_DEV: "true",
        PORTO_DATA_DIR: REMOTE_DATA_DIR,
        PORTO_MAIL_DIR: REMOTE_MAIL_DIR,
        PORTO_ADDR: `127.0.0.1:${REMOTE_PORT}`,
        PORTO_BASE_URL: REMOTE_URL,
        PORTO_AUTO_MIGRATE: "true",
        PORTO_LOG_LEVEL: "warn",
        PORTO_ADMIN_PASSWORD: OWNER.password,
      },
      reuseExistingServer: false,
      timeout: 60_000,
      stdout: "ignore",
      stderr: "pipe",
    },
  ],
});
