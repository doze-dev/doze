import { expect, test, type Page } from "@playwright/test";

// The Grid, against the MapGrid board, over the larder fixture: a row needs
// each column it has a dot in, and the tooltip quotes doze.hcl.
test.use({ viewport: { width: 1280, height: 900 } });

async function openGrid(page: Page) {
  await page.goto("/");
  await page.getByRole("tab", { name: /^larder/ }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("larder");
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "Grid" }).click();
  await expect(page.locator("table.g tbody tr")).toHaveCount(9);
}

// What doze.hcl (doze-examples/larder) refers to: each app's calls and uses.
const refs: Record<string, string[]> = {
  storefront: ["gateway"],
  gateway: ["identity", "catalog", "orders", "fulfilment", "recommendations", "redis"],
  identity: ["postgres", "kafka"],
  catalog: ["postgres", "aws"],
  orders: ["postgres", "kafka", "catalog"],
  payments: ["postgres", "kafka", "redis"],
  fulfilment: ["kafka", "aws", "orders"],
  recommendations: ["postgres", "kafka", "redis", "catalog"],
  notifications: ["kafka", "mailpit"],
};

test("grid: rows in flow order, a dot wherever doze.hcl refers, needed by underneath", async ({ page }, info) => {
  await openGrid(page);
  const grid = page.getByRole("grid", { name: "What each service needs" });
  await expect(grid.locator("tbody th b")).toHaveText(["storefront", "gateway", "identity", "fulfilment", "recommendations", "orders", "catalog", "payments", "notifications"]);
  await expect(grid.locator("thead tr.grph th:not(.fill)")).toHaveText([/Service\s*needs →/, "Other services", "Shared", "Before it"]);
  const dots = await grid.locator("td:has(.cell)").evaluateAll((tds) => tds.map((td) => td.getAttribute("aria-label")));
  const want = Object.entries(refs).flatMap(([a, ns]) => ns.map((n) => `${a} ${["redis", "postgres", "kafka", "aws", "mailpit"].includes(n) ? "uses" : "needs"} ${n}`));
  expect(dots.sort()).toEqual(want.sort());
  await expect(grid.locator("tfoot td:not(.fill)")).toHaveText(["needed by", "1", "1", "1", "1", "2", "3", "6", "5", "3", "2", "1", ""]);
  await expect(grid.locator('td[aria-label="orders itself"]')).toHaveText("—");
  await expect(grid.locator("tbody tr").nth(4).locator("td:not(.fill)").last()).toHaveText("2 tasks ✓");
  await page.screenshot({ path: `.desk-look/MapGrid-${info.project.name}.png` });
});

test("grid: a cell under the pointer draws the crosshair and says the line of doze.hcl", async ({ page }, info) => {
  await openGrid(page);
  const cell = page.locator('td[aria-label="orders needs catalog"]');
  await cell.hover();
  const tip = page.getByRole("tooltip");
  await expect(tip).toContainText("orders needs catalog · keeps it awake");
  await expect(tip.locator("code")).toHaveText("CATALOG_URL = app.catalog.url");
  await expect(tip).toContainText("⏎ opens catalog · o opens orders");
  await expect(cell).toHaveClass(/cross/);
  await expect(page.locator("tbody tr.selr th b")).toHaveText("orders");
  await expect(page.locator("thead th.selc b")).toHaveText("catalog");
  await page.screenshot({ path: `.desk-look/MapGrid-tip-${info.project.name}.png` });
  // A use quotes its line too, kept to the reference.
  await page.locator('td[aria-label="orders uses postgres"]').hover();
  await expect(tip.locator("code")).toHaveText('DATABASE_URL = "…${module.postgres.address}…"');
});

test("grid: the cells take the keyboard, and focus draws the same tooltip", async ({ page }) => {
  await openGrid(page);
  await page.keyboard.press("ArrowDown");
  const focused = page.locator("table.g td:focus");
  await expect(focused).toHaveAttribute("aria-label", "storefront needs gateway");
  await page.keyboard.press("ArrowDown");
  await expect(focused).toHaveAttribute("aria-label", "gateway itself");
  await page.keyboard.press("ArrowRight");
  await expect(focused).toHaveAttribute("aria-label", "gateway needs identity");
  await expect(page.getByRole("tooltip")).toContainText("IDENTITY_URL = app.identity.url");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("identity");
});

test("grid: the view is remembered for the stack", async ({ page }) => {
  await openGrid(page);
  await page.reload();
  await page.getByRole("tab", { name: /^larder/ }).click();
  await expect(page.getByRole("group", { name: "View" }).getByRole("button", { name: "Grid" })).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator("table.g")).toBeVisible();
});

test("grid: at 900 wide it scrolls inside its box with the first column held", async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 620 });
  await openGrid(page);
  const scroller = page.locator(".gridw .gscroll");
  const [sw, cw] = await scroller.evaluate((e) => [e.scrollWidth, e.clientWidth]);
  expect(sw).toBeGreaterThan(cw);
  await scroller.evaluate((e) => (e.scrollLeft = 400));
  const box = (await scroller.boundingBox())!;
  const first = (await page.locator("tbody tr").first().locator("th").boundingBox())!;
  expect(Math.abs(first.x - box.x)).toBeLessThan(2);
  await expect(page.locator("tbody tr").first().locator("th")).toBeInViewport();
  const keys = (await page.locator(".keys").boundingBox())!;
  expect(keys.y + keys.height).toBeLessThanOrEqual(620);
  // Back to tiles, as the stack began.
  await page.keyboard.press("1");
});
