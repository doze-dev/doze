import { defineConfig, devices } from "@playwright/test";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

// The app's page in a browser, against `doze open --serve --fixture`: no
// window, no daemon, the same Go views the app shows. WebKit is what the
// app renders in; Chromium is the second opinion. DOZE_BIN points at a
// built doze (make build); the default is a fresh build. The shop fixture
// serves most specs; the crash and modules fixtures each get a server of
// their own, matched to their spec files.
const bin = process.env.DOZE_BIN ?? "../../doze";
const port = Number(process.env.DOZE_E2E_PORT ?? 7731);
const viewport = { width: 1280, height: 800 };

// The empty spec opens a folder with no doze.hcl; the fixture server's setup
// writes the starter file there, and the spec reads it back.
// It is shared with the workers through the environment: this file runs
// again in each of them.
process.env.DOZE_E2E_EMPTY_DIR ??= fs.mkdtempSync(path.join(os.tmpdir(), "doze-empty-"));
const emptyDir = process.env.DOZE_E2E_EMPTY_DIR;

const fixtures = [
  { name: "shop", port, match: /(library|overview|instance|palette)\.spec\.ts/, args: "", fixture: "" },
  { name: "crash", port: port + 1, match: /crash\.spec\.ts/, args: "", fixture: "" },
  { name: "modules", port: port + 2, match: /console\.spec\.ts/, args: "", fixture: "" },
  { name: "empty", port: port + 3, match: /empty\.spec\.ts/, args: " " + emptyDir, fixture: "shop" },
  { name: "badconfig", port: port + 4, match: /config\.spec\.ts/, args: "", fixture: "" },
  { name: "portclash", port: port + 5, match: /portclash\.spec\.ts/, args: "", fixture: "" },
];

export default defineConfig({
  testDir: "./tests",
  outputDir: "./test-results",
  timeout: 20_000,
  // One worker: a fixture server has one focus and broadcasts its notices,
  // like the app it stands in for, so two pages on it would fight.
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["list"]] : "list",
  use: {
    viewport,
    colorScheme: "dark",
    trace: "retain-on-failure",
  },
  projects: fixtures.flatMap((f) => [
    {
      name: f.name === "shop" ? "webkit" : `${f.name}-webkit`,
      testMatch: f.match,
      use: { ...devices["Desktop Safari"], viewport, baseURL: `http://127.0.0.1:${f.port}` },
    },
    {
      name: f.name === "shop" ? "chromium" : `${f.name}-chromium`,
      testMatch: f.match,
      use: { ...devices["Desktop Chrome"], viewport, baseURL: `http://127.0.0.1:${f.port}` },
    },
  ]),
  webServer: fixtures.map((f) => ({
    command: `${bin} open --serve 127.0.0.1:${f.port} --fixture ${f.fixture || f.name}${f.args}`,
    url: `http://127.0.0.1:${f.port}/`,
    reuseExistingServer: false,
    timeout: 15_000,
  })),
});
