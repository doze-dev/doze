import { defineConfig, devices } from "@playwright/test";

// The app's page in a browser, against `doze open --serve --fixture`: no
// window, no daemon, the same Go views the app shows. WebKit is what the
// app renders in; Chromium is the second opinion. DOZE_BIN points at a
// built doze (make build); the default is a fresh build. The shop fixture
// serves most specs; the crash and modules fixtures each get a server of
// their own, matched to their spec files.
const bin = process.env.DOZE_BIN ?? "../../doze";
const port = Number(process.env.DOZE_E2E_PORT ?? 7731);
const viewport = { width: 1280, height: 800 };

const fixtures = [
  { name: "shop", port, match: /(home|parity)\.spec\.ts/ },
  { name: "crash", port: port + 1, match: /crash\.spec\.ts/ },
  { name: "modules", port: port + 2, match: /console\.spec\.ts/ },
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
    command: `${bin} open --serve 127.0.0.1:${f.port} --fixture ${f.name}`,
    url: `http://127.0.0.1:${f.port}/`,
    reuseExistingServer: false,
    timeout: 15_000,
  })),
});
