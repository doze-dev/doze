import { expect, test } from "@playwright/test";
import net from "node:net";

// The portclash fixture: api exits because 127.0.0.1:7799 is taken, and
// this spec is what holds it. Against the PortClash board: the page names
// the holder and offers the ways out.
test.use({ viewport: { width: 1280, height: 800 } });

test("an app whose port is taken names what has it", async ({ page }, info) => {
  const holder = net.createServer().listen(7799, "127.0.0.1");
  await new Promise((r) => holder.once("listening", r));
  try {
    await page.goto("/");
    await page.getByRole("tab", { name: /^shop/ }).click();
    await page.getByRole("group", { name: "View" }).getByRole("button", { name: "List" }).click();
    await page.locator(".ql .qr", { has: page.locator("b", { hasText: /^api$/ }) }).first().dblclick();
    const card = page.getByRole("region", { name: "Port 7799 is taken" });
    await expect(card).toContainText(`pid ${process.pid}`);
    await expect(card).toContainText(`Stop pid ${process.pid} and start api`);
    await expect(card).toContainText("Use another port");
    await expect(card.getByRole("button", { name: /Edit doze\.hcl/ })).toBeVisible();
    await expect(card).toContainText("doze never stops a process it did not start without asking");
    await page.screenshot({ path: `.desk-look/PortClash-${info.project.name}.png` });
  } finally {
    holder.close();
  }
});
