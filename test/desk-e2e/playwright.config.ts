import { defineConfig, devices } from "@playwright/test";

// The app's page in a browser, against `doze open --serve --fixture`: no
// window, no daemon, the same Go views the app shows. WebKit is what the
// app renders in; Chromium is the second opinion. DOZE_BIN points at a
// built doze (make build); the default is a fresh build.
const bin = process.env.DOZE_BIN ?? "../../doze";
const port = Number(process.env.DOZE_E2E_PORT ?? 7731);

export default defineConfig({
  testDir: "./tests",
  outputDir: "./test-results",
  timeout: 20_000,
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["list"]] : "list",
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    viewport: { width: 960, height: 640 },
    colorScheme: "dark",
    trace: "retain-on-failure",
  },
  projects: [
    { name: "webkit", use: { ...devices["Desktop Safari"], viewport: { width: 960, height: 640 } } },
    { name: "chromium", use: { ...devices["Desktop Chrome"], viewport: { width: 960, height: 640 } } },
  ],
  webServer: {
    command: `${bin} open --serve 127.0.0.1:${port} --fixture ${process.env.DOZE_FIXTURE ?? "shop"}`,
    url: `http://127.0.0.1:${port}/`,
    reuseExistingServer: false,
    timeout: 15_000,
  },
});
