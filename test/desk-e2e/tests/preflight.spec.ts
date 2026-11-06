import { expect, test } from "@playwright/test";
import net from "node:net";

// Up looks first (D19, the PreFlight board), over the shop fixture's
// stopped acme stack: its web declares port 3000, and this spec holds it.
test.use({ viewport: { width: 1280, height: 800 } });

test("a port another program holds stops Up and asks, with the ways out", async ({ page }, info) => {
  const holder = net.createServer().listen(3000, "127.0.0.1");
  await new Promise((r) => holder.once("listening", r));
  try {
    await page.goto("/");
    const acme = page.locator(".lrow", { hasText: "acme" }).first();
    await acme.getByRole("switch").click();
    const sheet = page.getByRole("dialog", { name: "Before acme starts" });
    await expect(sheet).toContainText("one thing is in the way");
    await expect(sheet).toContainText("The folder is there, and doze.hcl loads");
    await expect(sheet).toContainText("Port 3000 for web is taken");
    await expect(sheet).toContainText(`(pid ${process.pid})`);
    await expect(sheet.getByRole("button", { name: `Stop pid ${process.pid}, then Up` })).toBeVisible();
    await expect(sheet.getByRole("button", { name: "Up without web" })).toBeVisible();
    await expect(sheet.getByRole("button", { name: "Up anyway" })).toBeVisible();
    await expect(sheet).toContainText("nothing has started yet");
    await page.screenshot({ path: `.desk-look/PreFlight-${info.project.name}.png` });
    await page.keyboard.press("Escape");
    await expect(sheet).toBeHidden();
    // Nothing started: the switch is still off.
    await expect(acme.getByRole("switch")).toHaveAttribute("aria-checked", "false");
  } finally {
    holder.close();
  }
});
