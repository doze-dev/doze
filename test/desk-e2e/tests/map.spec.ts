import { expect, test, type Page } from "@playwright/test";

// The Map, against the MapFlow and MapFlowSelected boards, over the larder
// fixture: services that call services, an event lane, a shared band.
test.use({ viewport: { width: 1280, height: 900 } });

async function openMap(page: Page) {
  await page.goto("/");
  await page.getByRole("tab", { name: /^larder/ }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("larder");
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "Map" }).click();
  await expect(page.locator(".flow .node")).toHaveCount(9);
}

/** behindCards samples every drawn line and names any point that is inside a card that is not one of its ends. */
async function behindCards(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const canvas = document.querySelector(".flow .canvas") as HTMLElement;
    const at = canvas.getBoundingClientRect();
    const cards = [...document.querySelectorAll<HTMLElement>(".flow .node, .flow .inf")].map((c) => {
      const r = c.getBoundingClientRect();
      return { name: c.dataset.name ?? "", x: r.left - at.left, y: r.top - at.top, w: r.width, h: r.height };
    });
    const bad: string[] = [];
    for (const p of document.querySelectorAll<SVGPathElement>(".flow svg path[data-from]")) {
      const from = p.dataset.from;
      const to = p.dataset.to;
      const len = p.getTotalLength();
      for (let l = 0; l <= len; l += 3) {
        const q = p.getPointAtLength(l);
        for (const c of cards) {
          if (c.name === from || c.name === to) continue;
          if (q.x > c.x + 2 && q.x < c.x + c.w - 2 && q.y > c.y + 2 && q.y < c.y + c.h - 2) {
            bad.push(`${from}>${to} behind ${c.name}`);
            l = len + 1;
            break;
          }
        }
      }
    }
    return bad;
  });
}

test("map: calls left to right by depth, the event lane, the shared band, no line behind a card", async ({ page }, info) => {
  await openMap(page);
  const map = page.getByRole("group", { name: /The stack as a map/ });
  await expect(map.locator(".cap")).toHaveText([/Who calls whom\s*left to right/, /Nobody calls these\s*they work from events/, /Shared by the stack/]);
  // Columns by depth: storefront, gateway, then identity · fulfilment ·
  // recommendations, then orders, then catalog.
  const left = async (name: string) => (await map.locator(`.node[data-name="${name}"]`).boundingBox())!.x;
  const xs = await Promise.all(["storefront", "gateway", "identity", "fulfilment", "recommendations", "orders", "catalog"].map(left));
  expect(xs[0]).toBeLessThan(xs[1]);
  expect(xs[1]).toBeLessThan(xs[2]);
  expect(xs[2]).toBe(xs[3]);
  expect(xs[3]).toBe(xs[4]);
  expect(xs[4]).toBeLessThan(xs[5]);
  expect(xs[5]).toBeLessThan(xs[6]);
  // The lane, and the band below every app card.
  await expect(map.locator(".node[data-name=payments]")).toContainText("postgres");
  const laneTop = (await map.locator(".node[data-name=payments]").boundingBox())!.y;
  expect(laneTop).toBeGreaterThan((await map.locator(".node[data-name=recommendations]").boundingBox())!.y);
  await expect(map.locator(".inf")).toHaveCount(5);
  await expect(map.locator(".inf[data-name=kafka]")).toContainText("used by 6");
  await expect(map.locator(".inf[data-name=mailpit]")).toContainText("used by 1 · notifications");
  // Chips, the folded tasks, the language badges.
  await expect(map.locator(".node[data-name=recommendations] .chip")).toHaveText(["postgres", "kafka", "redis"]);
  await expect(map.locator(".node[data-name=orders] .fold")).toHaveAttribute("title", /^before it: migrate-orders ✓ 1\dm ago$/);
  await expect(map.locator(".node[data-name=fulfilment] .lang")).toHaveText("py");
  await expect(map.locator(".node[data-name=storefront]")).toContainText("no shared services");
  // Nine calls, each a line, none behind a card; no dashed line yet.
  await expect(map.locator("path.edge")).toHaveCount(9);
  await expect(map.locator("path.use")).toHaveCount(0);
  expect(await behindCards(page)).toEqual([]);
  await expect(page.locator(".flow .legend")).toContainText("calls, from references in doze.hcl");
  await page.screenshot({ path: `.desk-look/MapFlow-${info.project.name}.png` });
});

test("map: a selected card lights what it calls, its callers and what it uses, with dashed lines and its detail; esc clears", async ({ page }, info) => {
  await openMap(page);
  const map = page.getByRole("group", { name: /The stack as a map/ });
  await map.locator(".node[data-name=orders]").click();
  await expect(map.locator(".node.sel")).toHaveAttribute("data-name", "orders");
  const detail = page.getByRole("region", { name: "orders, selected" });
  await expect(detail).toContainText("Go · 47 MB · awake");
  await expect(detail.locator(".dr")).toHaveText([/needs\s*catalog · and postgres, kafka below/, /needed by\s*gateway, fulfilment · they keep it awake/, /before it\s*migrate-orders ✓ 1\dm ago/]);
  // Lit: its callers and what it calls; the band cards it uses are hot;
  // the rest steps back.
  for (const n of ["gateway", "fulfilment", "catalog"]) await expect(map.locator(`.node[data-name=${n}]`)).not.toHaveClass(/faded/);
  for (const n of ["storefront", "identity", "recommendations", "payments", "notifications"]) await expect(map.locator(`.node[data-name=${n}]`)).toHaveClass(/faded/);
  await expect(map.locator(".inf.hot")).toHaveCount(2);
  await expect(map.locator(".node[data-name=orders] .chip.hot")).toHaveText(["postgres", "kafka"]);
  // Dashed lines down to postgres and kafka, clear of every card.
  await expect(map.locator("path.use")).toHaveCount(2);
  await expect(map.locator('path.use[data-to="postgres"]')).toHaveCount(1);
  await expect(map.locator("path.edge:not(.dim)")).toHaveCount(3);
  expect(await behindCards(page)).toEqual([]);
  await expect(page.locator(".keys")).toContainText("clear the selection");
  await page.screenshot({ path: `.desk-look/MapFlowSelected-${info.project.name}.png` });

  // Esc clears the selection and stays on the stack; the next esc goes home.
  await page.keyboard.press("Escape");
  await expect(map.locator(".node.sel")).toHaveCount(0);
  await expect(page.locator(".flow .detail")).toHaveCount(0);
  await expect(page.locator(".flow .faded")).toHaveCount(0);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("larder");
});

test("map: the arrows move the selection between cards as they are drawn, ⏎ opens", async ({ page }) => {
  await openMap(page);
  const sel = page.locator(".flow .node.sel, .flow .inf.sel");
  await page.keyboard.press("ArrowRight");
  await expect(sel).toHaveAttribute("data-name", "storefront");
  await page.keyboard.press("ArrowRight");
  await expect(sel).toHaveAttribute("data-name", "gateway");
  await page.keyboard.press("ArrowRight");
  await expect(sel).toHaveAttribute("data-name", "fulfilment");
  await page.keyboard.press("ArrowUp");
  await expect(sel).toHaveAttribute("data-name", "identity");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await expect(sel).toHaveAttribute("data-name", "recommendations");
  // A band card is selectable too: what uses it lights up.
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await expect(sel).toHaveClass(/inf/);
  await page.keyboard.press("Escape");
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("gateway");
});

test("map: the view is remembered for the stack", async ({ page }) => {
  await openMap(page);
  await page.reload();
  await page.getByRole("tab", { name: /^larder/ }).click();
  await expect(page.getByRole("group", { name: "View" }).getByRole("button", { name: "Map" })).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator(".flow .node")).toHaveCount(9);
});

test("map: at 900 wide the drawing scrolls inside its box", async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 620 });
  await openMap(page);
  const scroller = page.locator(".flow .mscroll");
  const [sw, cw] = await scroller.evaluate((e) => [e.scrollWidth, e.clientWidth]);
  expect(sw).toBeGreaterThan(cw);
  // The keys stay in the window, under the box.
  const keys = (await page.locator(".keys").boundingBox())!;
  expect(keys.y + keys.height).toBeLessThanOrEqual(620);
  await page.locator(".flow .node[data-name=orders]").click();
  await expect(page.locator(".flow .detail")).toBeInViewport();
});
