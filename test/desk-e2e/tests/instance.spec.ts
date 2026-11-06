import { expect, test } from "@playwright/test";

// Inside a service, against the ModuleOverview, AppOverview, ModuleAsleep,
// InstanceLogs, TaskRan, TaskNotRun, ConfirmSleep and Reset boards, over
// the shop fixture.
test.use({ viewport: { width: 1280, height: 800 } });

async function open(page: import("@playwright/test").Page, name: string) {
  await page.goto("/");
  await page.getByRole("tab", { name: /^shop/ }).click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("shop");
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "List" }).click();
  await page.locator(".ql .qr", { has: page.locator("b", { hasText: new RegExp(`^${name}$`) }) }).first().dblclick();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(name);
}

test("a module's overview: connect, the snippets, why it's awake, the charts", async ({ page }, info) => {
  await open(page, "db");
  await expect(page.locator(".head .path")).toContainText("postgres 17 · awake for 14m · pid 612");
  await expect(page.locator(".ptab")).toHaveText(["Overview", "Logs"]);
  await expect(page.locator(".side .si.on")).toContainText("db");
  const connect = page.locator(".card", { has: page.getByRole("heading", { name: "Connect", exact: false }) }).first();
  await expect(connect).toContainText("postgres://shop@db.shop.doze:5432/shop");
  await expect(connect).toContainText("DATABASE_URL");
  await expect(connect).toContainText("db.shop.doze:5432");
  await expect(connect).toContainText("~/.doze/shop/db");
  const code = page.locator(".card", { hasText: "From your code" });
  await expect(code.getByRole("tab")).toHaveText(["Node", "Python", "Go", "Shell", ".env", "psql"]);
  await code.getByRole("tab", { name: "psql" }).click();
  await expect(code.locator(".code")).toContainText("psql");
  const why = page.locator(".card", { hasText: "Why it's awake" });
  await expect(why).toContainText("api needs it");
  await expect(why).toContainText("4 connections");
  await expect(why).toContainText("With neither, it sleeps after 5:00");
  await expect(page.locator(".card", { hasText: "Memory · last" })).toContainText("96 MB");
  await expect(page.locator(".card", { hasText: "CPU · last" })).toContainText("cores");
  await expect(page.locator(".card", { hasText: "Recent" })).toContainText("woke");
  await page.screenshot({ path: `.desk-look/ModuleOverview-${info.project.name}.png` });

  // Sleep asks, in the ConfirmSleep board's words; esc keeps it awake.
  await page.getByRole("button", { name: /^Sleep/ }).click();
  const dlg = page.getByRole("dialog", { name: "Sleep db?" });
  await expect(dlg).toContainText("api is running and needs it, and 4 connections are open to it right now.");
  await expect(dlg).toContainText("Connections drop.");
  await expect(dlg).toContainText("Kept. Nothing is deleted.");
  await expect(dlg.locator(".btn")).toHaveText([/Sleep db and api/, "Sleep db only", "Turn db off", /Cancel/]);
  await expect(dlg).toContainText("doze only asks when something else is affected.");
  await page.screenshot({ path: `.desk-look/ConfirmSleep-${info.project.name}.png` });
  await page.keyboard.press("Escape");
  await expect(dlg).toBeHidden();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("db");
});

test("reset wants the name typed and defaults to Cancel, as the Reset board does", async ({ page }, info) => {
  await open(page, "db");
  await page.keyboard.press("ControlOrMeta+k");
  await page.getByRole("textbox", { name: "A service, a stack, or what to do" }).fill("reset db");
  await expect(page.locator(".pal .it.on")).toContainText("db");
  await expect(page.locator(".pal .it.on")).toContainText("wipes its data");
  await page.keyboard.press("Enter");
  const dlg = page.getByRole("dialog", { name: /reset db/ });
  await expect(dlg).toHaveClass(/danger/);
  await expect(dlg).toContainText("deletes");
  await expect(dlg).toContainText("everything in ~/.doze/shop/db");
  await expect(dlg).toContainText("db starts empty on its next wake");
  await expect(dlg).toContainText("none. There is no snapshot.");
  const resetBtn = dlg.getByRole("button", { name: "Reset db" });
  await expect(resetBtn).toBeDisabled();
  await expect(dlg).toContainText("From a script: doze reset db --yes");
  await dlg.getByLabel("Type db to confirm").fill("d");
  await expect(resetBtn).toBeDisabled();
  await page.screenshot({ path: `.desk-look/Reset-${info.project.name}.png` });
  await dlg.getByLabel("Type db to confirm").fill("db");
  await expect(resetBtn).toBeEnabled();
  await page.keyboard.press("Enter");
  await expect(dlg).toBeHidden();
});

test("an app's overview: reach it, the domain, and its logs tab", async ({ page }, info) => {
  await open(page, "web");
  await expect(page.locator(".head .path")).toContainText("process · pnpm dev");
  const reach = page.locator(".card", { hasText: "Reach it" });
  await expect(reach).toContainText("https://dev.shop.example");
  await expect(reach).toContainText("*.dev.shop.example");
  await expect(page.getByRole("button", { name: /Open dev.shop.example/ })).toBeVisible();
  await page.screenshot({ path: `.desk-look/AppOverview-${info.project.name}.png` });

  await page.locator('.ptab[data-tab="logs"]').click();
  await expect(page.getByRole("group", { name: "Level" })).toBeVisible();
  await expect(page.locator(".lg .ln").first()).toBeVisible();
  await page.getByRole("group", { name: "Time" }).getByRole("button", { name: "Clock" }).click();
  await expect(page.locator(".lg .ln .dim").first()).toHaveText(/^\d\d:\d\d:\d\d$/);
  await page.screenshot({ path: `.desk-look/InstanceLogs-${info.project.name}.png` });
});

test("an asleep module's page says so and offers Wake", async ({ page }, info) => {
  await open(page, "search");
  await expect(page.locator(".head .path")).toContainText("asleep");
  await expect(page.getByRole("heading", { level: 2 })).toHaveText("search is asleep.");
  await expect(page.locator(".empty")).toContainText("It wakes on the next connection, and sleeps again when nothing holds it. Its data stays in between.");
  await expect(page.locator(".empty").getByRole("button", { name: /Wake/ })).toBeVisible();
  await page.screenshot({ path: `.desk-look/ModuleAsleep-${info.project.name}.png` });
});

test("a task that ran: its last run, the runs kept, as the TaskRan board does", async ({ page }, info) => {
  await open(page, "migrate");
  await expect(page.locator(".head .path")).toContainText("task · last run 2m ago · exit 0 in 1.2s");
  await expect(page.getByRole("button", { name: "Copy output" })).toBeVisible();
  await expect(page.getByRole("button", { name: /Run again/ })).toBeVisible();
  await expect(page.locator(".ptab")).toHaveText(["Last run", "Runs · 3"]);
  const output = page.locator(".card", { hasText: "Output" }).first();
  await expect(output).toContainText("run 3");
  await expect(output).toContainText("exit 0");
  await expect(output).toContainText("14 migrations already applied");
  const runs = page.getByRole("table", { name: "Runs" });
  await expect(runs.locator(".rr.h")).toHaveText(/When\s*Took\s*Exit\s*Started by/);
  await expect(runs.locator(".rr:not(.h)")).toHaveCount(3);
  await expect(runs.locator(".rr:not(.h)").nth(0)).toContainText("0015_orders_index.sql");
  await expect(runs.locator(".rr:not(.h)").nth(1)).toContainText("1");
  await expect(runs.locator(".rr:not(.h)").nth(2)).toContainText("you asked");
  await expect(page.locator(".model")).toContainText("A task keeps nothing awake once it ends.");
  await page.screenshot({ path: `.desk-look/TaskRan-${info.project.name}.png` });
  // The newest run is shown; Output on another, the failed one, shows that
  // run's output in the card instead.
  await expect(runs.locator(".rr:not(.h)").first().getByRole("button", { name: "Shown" })).toBeVisible();
  await runs.locator(".rr:not(.h)").nth(1).getByRole("button", { name: "Output" }).click();
  await expect(runs.locator(".rr:not(.h)").nth(1).getByRole("button", { name: "Shown" })).toBeVisible();
  await expect(output).toContainText("exit 1");
});

test("a task not yet run: what Run will do, as the TaskNotRun board does", async ({ page }, info) => {
  await open(page, "seed");
  await expect(page.locator(".head .path")).toContainText("task · not run yet");
  await expect(page.getByRole("button", { name: "Edit doze.hcl" })).toBeVisible();
  await expect(page.getByRole("button", { name: /^Run/ })).toBeVisible();
  await expect(page.locator(".ptab")).toHaveText(["Overview", "Runs · none yet"]);
  await expect(page.getByRole("heading", { level: 2 })).toHaveText("seed has not run yet.");
  await expect(page.locator(".lrow", { hasText: "run the command" })).toContainText("with the stack's variables");
  await expect(page.locator(".lrow", { hasText: "report" })).toContainText("exit code, how long it took");
  await expect(page.locator(".model")).toContainText("From a terminal: doze task seed");
  await expect(page.locator(".card", { hasText: "Output" })).toContainText("nothing yet");
  await page.screenshot({ path: `.desk-look/TaskNotRun-${info.project.name}.png` });
  // ⏎ runs it and says so.
  await page.keyboard.press("Enter");
  await expect(page.locator(".toast").first()).toContainText("seed");
});

test("a service's own web console is framed in its tab, with the browser a click away", async ({ page }, info) => {
  await open(page, "mailpit");
  await page.getByRole("button", { name: "Wake and open the console" }).click();
  const frame = page.locator(".frame");
  await expect(frame.locator(".ctop")).toContainText("mailpit");
  await expect(frame.locator(".ctop")).toContainText("http://mailpit.shop.doze:8025");
  await expect(frame.getByRole("button", { name: /Open in browser/ })).toBeVisible();
  await expect(frame.getByRole("button", { name: /Copy address/ })).toBeVisible();
  const iframe = frame.locator("iframe");
  await expect(iframe).toHaveAttribute("src", /mailpit\.shop\.doze:8025\/?\?theme=(dark|light)/);
  await page.screenshot({ path: `.desk-look/ConsoleTab-web-${info.project.name}.png` });
  // Back to sleep, as the fixture began, so other checks see what they expect.
  const stacks = await (await page.request.post("/api/stacks", { data: {} })).json();
  const key = stacks.find((s: { name: string }) => s.name === "shop").key;
  await page.request.post("/api/do", { data: { key, verb: "sleep", target: "mailpit" } });
});
