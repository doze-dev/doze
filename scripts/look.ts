// look is how the dash is checked against its boards without a person.
//
//   bun scripts/look.ts                  every screen
//   bun scripts/look.ts palette focus-db some screens
//
// For each screen it writes <out>/<screen>.png: the board on the left, what the
// dash draws on the right, both through the same browser at the board's size.
// Screens and their boards are the table below; a screen's state is built by
// TestShots in internal/dash/home_test.go.
import { mkdirSync, existsSync } from "node:fs";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const root = resolve(import.meta.dir, "..");
const out = process.env.LOOK_OUT ?? join(root, ".look");
mkdirSync(out, { recursive: true });

// screen -> board, and the size of the board in px.
const screens: Record<string, { board: string; w: number; h: number }> = {
  home: { board: "Main", w: 1200, h: 760 },
  narrow: { board: "Narrow", w: 716, h: 564 },
  crash: { board: "Crash", w: 1200, h: 760 },
  "focus-db": { board: "Focus", w: 1200, h: 760 },
  "focus-api": { board: "View-App", w: 1200, h: 760 },
  "focus-task": { board: "View-Task", w: 1200, h: 760 },
  "focus-web": { board: "View-Web", w: 1200, h: 760 },
  palette: { board: "Palette", w: 1200, h: 760 },
  sleep: { board: "Confirm-Sleep", w: 1200, h: 760 },
  reset: { board: "Confirm-Reset", w: 1200, h: 760 },
};

const want = process.argv.slice(2);
const names = want.length ? want : Object.keys(screens);
for (const n of names) {
  if (!screens[n]) {
    console.error(`unknown screen ${n}; known: ${Object.keys(screens).join(", ")}`);
    process.exit(2);
  }
}

// The dash draws its frames. A terminal this wide and tall is what a board of
// this many pixels is, at 14px type and a 17px line.
const t = spawnSync("go", ["test", "./internal/dash/", "-run", "TestShots", "-count=1"], {
  cwd: root,
  env: { ...process.env, DOZE_SHOT_DIR: out },
  encoding: "utf8",
});
if (t.status !== 0) {
  console.error(t.stdout + t.stderr);
  process.exit(1);
}

const CHROME = process.env.CHROME ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const shot = (src: string, png: string, w: number, h: number) => {
  const r = spawnSync("bun", [join(root, "scripts/shot.ts"), src, png, String(w), String(h)], { encoding: "utf8" });
  if (r.status !== 0) {
    console.error(r.stdout + r.stderr);
    process.exit(1);
  }
};

for (const n of names) {
  const { board, w, h } = screens[n];
  const boardPng = join(out, `${n}.board.png`);
  const dashPng = join(out, `${n}.dash.png`);
  shot(join(root, "docs/design/next/boards", `${board}.dc.html`), boardPng, w, h);
  shot(join(out, `${n}.ans`), dashPng, w, h);
  // both images on one page, so one read sees both
  const html = join(out, `${n}.pair.html`);
  Bun.write(
    html,
    `<!doctype html><body style="margin:0;background:#000;display:flex;gap:8px">` +
      `<img src="file://${boardPng}" width="${w}" height="${h}">` +
      `<img src="file://${dashPng}" width="${w}" height="${h}"></body>`,
  );
  const pair = join(out, `${n}.png`);
  const r = spawnSync(CHROME, [
    "--headless=new", "--disable-gpu", "--hide-scrollbars", "--force-device-scale-factor=1",
    `--window-size=${w * 2 + 8},${h}`, `--screenshot=${pair}`, "file://" + html,
  ], { encoding: "utf8" });
  if (r.status !== 0 || !existsSync(pair)) {
    console.error(r.stderr);
    process.exit(1);
  }
  console.log(pair);
}
