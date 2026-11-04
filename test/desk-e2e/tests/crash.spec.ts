import { expect, test } from "@playwright/test";

// The crash fixture (shop with api in a crash loop), against the Crash
// board: the red dot on the tab and in the sidebar, the head line, the
// It exited card, the tries and the next try, its last run.

test("a crashing app shows as the Crash board does", async ({ page }, info) => {
  await page.goto("/");
  const tab = page.locator(".stab", { hasText: "shop" });
  await expect(tab.locator(".dot")).toHaveClass(/r/);
  await expect(tab.locator(".badge")).toHaveText("1");
  const api = page.locator(".tr.row", { hasText: "api" }).first();
  await expect(api).toContainText("crashing");
  await expect(api).toContainText("try 3 of 5");
  await api.dblclick();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("api");
  await expect(page.locator(".head .path")).toContainText("go run ./cmd/api · crashing");
  await expect(page.locator(".side .space.compact.on .badge")).toHaveText("!");
  await expect(page.getByRole("button", { name: /Retry now/ })).toBeVisible();
  const exited = page.locator(".card", { has: page.getByRole("heading", { name: "It exited", exact: true }) });
  await expect(exited).toContainText("panic: STRIPE_KEY is required");
  await expect(exited.getByRole("button", { name: "Copy the error" })).toBeVisible();
  const next = page.locator(".card", { has: page.getByRole("heading", { name: "What doze does next", exact: true }) });
  await expect(next).toContainText("try 3 of 5");
  await expect(next).toContainText(/next try in \d+ s/);
  await expect(next).toContainText("nothing else in the stack is touched");
  const last = page.locator(".card", { has: page.getByRole("heading", { name: "Its last run", exact: true }) });
  await expect(last).toContainText("panic: STRIPE_KEY is required");
  await page.screenshot({ path: `.desk-look/Crash-${info.project.name}.png` });
});
