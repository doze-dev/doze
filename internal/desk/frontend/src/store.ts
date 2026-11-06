// What the page holds, in a TanStack Store: the stacks for the tab strip,
// where the user is, and the view in focus, each replaced whole by Go's
// pushes. Go computes every fact (D16); this only keeps the latest and lets
// each component subscribe to the slice it draws, so a push re-renders only
// what it changed.
import { useEffect } from "react";
import { Store, useSelector } from "@tanstack/react-store";
import { transport, type LogsChanged, type Nav, type serve, type view } from "./transport";

export type Page = "library" | "home" | "instance" | "logs" | "recent" | "empty" | "settings" | "timeline";

export interface Where {
  key: string;
  page: Page;
  instance?: string;
  /** dir is the folder an empty page is for. */
  dir?: string;
  /** section is the part of Settings shown: general, machine, about. */
  section?: string;
}

/** Theme is a colour theme; Mode is dark, light, or system (follow macOS). The two are chosen apart. */
export type Theme = "violet" | "teal" | "burgundy" | "amber" | "slate";
export type Mode = "system" | "dark" | "light";

export interface Toast {
  id: number;
  text: string;
  bad: boolean;
  shownAt: number;
}

export interface State {
  stacks: serve.StackSummary[];
  version: string;
  where: Where | null;
  /** library is the first tab: every stack, the help strip, the machine line. */
  library: view.Library | null;
  home: view.Home | null;
  instance: view.Instance | null;
  /** empty is the page of a folder with no doze.hcl yet. */
  empty: view.Empty | null;
  /** selected is the row the home table has picked; its logs fill the dock. */
  selected: string;
  /** choosing is the stack whose start sheet is open, or "". */
  choosing: string;
  /** show, on the merged logs page, is the instances ticked in the sidebar; null is all. */
  show: string[] | null;
  /** logsVersion bumps when the focused logs changed; the logs views refetch. */
  logsVersion: number;
  logsTotal: number;
  error: string | null;
  /** dialog is the confirmation on screen, from Go, in the dash's words. */
  dialog: { key: string; d: view.Dialog } | null;
  /** palette is whether ⌘K is open; help whether the Keys sheet is. */
  palette: boolean;
  help: boolean;
  /** toasts is what was said and has not faded yet. */
  toasts: Toast[];
  theme: Theme;
  mode: Mode;
  /** dark is the mode as resolved: what the page is drawn in now. */
  dark: boolean;
  /** history is where the page has been, oldest first; future what back left behind. ⌘[ and ⌘] walk them. */
  history: Where[];
  future: Where[];
  /** tab is the instance page's tab; sel the selected row of each console table, by block id. Go follows both. */
  tab: string;
  sel: Record<string, number>;
  /** closed is the stacks whose tab was closed: still running, listed in the Library, back in the strip when opened again. */
  closed: string[];
  /** attention is Needs you (D19), as Go last pushed it; bell says whether its panel is open. */
  attention: view.Attention | null;
  bell: boolean;
  /** alerts are red items that appeared while the window was open: a toast each, with its fix, until acted on or closed. */
  alerts: view.Item[];
  /** away is what happened while the window was closed, until Got it or the bell. */
  away: view.Away | null;
  /** preflight is the checks that stopped an Up, with what was asked, until a way out or Cancel. */
  preflight: { key: string; action: string; names: string[]; p: view.Preflight } | null;
}

export const store = new Store<State>({
  stacks: [],
  version: "",
  where: null,
  library: null,
  home: null,
  instance: null,
  empty: null,
  selected: "",
  choosing: "",
  show: null,
  logsVersion: 0,
  logsTotal: 0,
  error: null,
  dialog: null,
  palette: false,
  help: false,
  toasts: [],
  theme: "violet",
  mode: "system",
  dark: true,
  history: [],
  future: [],
  tab: "",
  sel: {},
  closed: readClosed(),
  attention: null,
  bell: false,
  alerts: [],
  away: null,
  preflight: null,
});

// seenRed is every red item the page has seen, so only a new one toasts;
// the first push only primes it.
let seenRed: Set<string> | null = null;

/** gotAttention takes Go's list: new red items become alerts, and an alert whose item left goes too. */
function gotAttention(a: view.Attention) {
  const items = a.items ?? [];
  const red = items.filter((i) => i.red && !i.fixed);
  set((s) => {
    const fresh = seenRed ? red.filter((i) => !seenRed!.has(i.id + "|" + i.print)) : [];
    const live = new Set(red.map((i) => i.id));
    const alerts = [...s.alerts.filter((x) => live.has(x.id)).map((x) => red.find((i) => i.id === x.id) ?? x), ...fresh.filter((i) => !s.alerts.some((x) => x.id === i.id))];
    return { attention: a, alerts: s.bell ? [] : alerts.slice(-3) };
  });
  seenRed = new Set([...(seenRed ?? []), ...red.map((i) => i.id + "|" + i.print)]);
}

/** closeAlert takes an alert's toast away; the item stays in the bell. */
export const closeAlert = (id: string) => set((s) => ({ alerts: s.alerts.filter((x) => x.id !== id) }));

/** notNow puts an item aside until what it says changes. */
export async function notNow(it: view.Item) {
  closeAlert(it.id);
  const t = await transport();
  await t.notNow(it.id, it.print);
}

/** showItem goes to the page that explains an item, and closes the bell. */
export async function showItem(it: view.Item) {
  set({ bell: false });
  closeAlert(it.id);
  const p = it.show;
  if (!p) return;
  if (p.page === "settings") await go({ key: "", page: "settings", section: p.section });
  else if (p.page === "instance") await go({ key: p.key, page: "instance", instance: p.instance });
  else await go({ key: p.key, page: "home" });
}

/** fixIt presses an item's fix: the same calls, and the same dialogs, the pages use. */
export async function fixIt(it: view.Item, f: view.Fix) {
  const t = await transport();
  closeAlert(it.id);
  try {
    switch (f.kind) {
      case "do":
        set({ bell: false });
        await ask(it.key ?? "", it.instance ?? "", f.verb ?? "");
        break;
      case "stack":
        set({ bell: false });
        if (f.verb === "remove" || f.verb === "down") await askStack(it.key ?? "", f.verb);
        else await stackAction(it.key ?? "", f.verb ?? "");
        break;
      case "holder":
        await t.stopHolder(it.key ?? "", it.instance ?? "", f.pid ?? 0);
        break;
      case "repair":
        await repair();
        break;
      case "clean":
        await t.clearLeftover("all");
        break;
      case "dismiss":
        await t.dismiss(it.key ?? "");
        break;
      case "edit":
        t.open("file://" + (f.path ?? ""));
        break;
      case "copy":
        await copy(f.text ?? "", "the command");
        break;
      case "show":
        await showItem(it);
        break;
    }
  } catch (e) {
    await say(String(e).replace(/^Error: /, ""), true);
  }
}

const closedKey = "doze.closedTabs";
function readClosed(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(closedKey) ?? "[]");
    return Array.isArray(v) ? v.filter((x) => typeof x === "string") : [];
  } catch {
    return [];
  }
}
function saveClosed(closed: string[]) {
  set({ closed });
  try {
    localStorage.setItem(closedKey, JSON.stringify(closed));
  } catch {
    // no storage
  }
}

/** closeTab takes a stack's tab off the strip; the stack keeps running. Closing the tab on screen goes to the tab before it, or the Library. */
export async function closeTab(key: string) {
  const s = get();
  const tabs = tabStacks(s.stacks, s.where?.key ?? "", s.closed);
  const i = tabs.findIndex((x) => x.key === key);
  saveClosed([...s.closed.filter((k) => k !== key), key]);
  if (s.where?.key === key) {
    const next = tabs[i - 1] ?? tabs[i + 1];
    await go(next && next.key !== key ? { key: next.key, page: "home" } : { key: "", page: "library" });
  }
}

/** set patches the store, outside React or in. */
export function set(patch: Partial<State> | ((s: State) => Partial<State>)) {
  store.setState((prev) => ({ ...prev, ...(typeof patch === "function" ? patch(prev) : patch) }));
}
const get = () => store.get();

/** useStore subscribes a component to one selection of the state. */
export function useStore<T>(select: (s: State) => T): T {
  return useSelector(store, select);
}

/** useSlice subscribes to several fields at once, re-rendering only when one changes. */
export function useSlice<T extends object>(pick: (s: State) => T): T {
  return useSelector(store, pick, { compare: shallow });
}

function shallow<T extends object>(a: T, b: T): boolean {
  if (Object.is(a, b)) return true;
  const ka = Object.keys(a) as Array<keyof T>;
  const kb = Object.keys(b) as Array<keyof T>;
  return ka.length === kb.length && ka.every((k) => Object.is(a[k], b[k]));
}

// nav numbers each navigation, so a slow answer for the last page does not
// land on the next one.
let nav = 0;

/** focus tells Go what is on screen: the page, and on home the selected row. */
async function focus(where: Where, selected: string) {
  const t = await transport();
  const s = get();
  const onInstance = where.page === "instance";
  await t.focus({
    key: where.key,
    page: where.page,
    instance: where.page === "home" ? selected : (where.instance ?? ""),
    tab: onInstance ? s.tab : "",
    sel: onInstance ? s.sel : {},
  });
}

/** setConsole says which tab the instance page shows and which rows its tables have picked; Go follows. */
export async function setConsole(tab: string, sel: Record<string, number>) {
  const s = get();
  set({ tab, sel });
  if (s.where?.page === "instance") await focus(s.where, s.selected);
}

/** go moves the page; Go follows with its pushes. A plain move is remembered for ⌘[; back and forward are not. */
export async function go(where: Where, how: "push" | "back" | "forward" = "push") {
  const n = ++nav;
  const t = await transport();
  const s = get();
  const sameStack = s.where?.key === where.key;
  const trail = how === "push" && s.where && !samePlace(s.where, where) ? { history: [...s.history, s.where].slice(-50), future: [] } : {};
  if (where.key && s.closed.includes(where.key)) saveClosed(s.closed.filter((k) => k !== where.key));
  if (where.page !== "empty") {
    try {
      localStorage.setItem(lastKey, JSON.stringify({ key: where.key, page: where.page, instance: where.instance }));
    } catch {
      // no storage
    }
  }
  set({ ...trail, where, home: sameStack ? s.home : null, instance: null, error: null, show: sameStack ? s.show : null, dialog: null, palette: false, tab: "", sel: {} });
  await focus(where, sameStack ? s.selected : "");
  // The first paint does not wait for a push.
  try {
    if (where.page === "library" || where.page === "settings") {
      const library = await t.library();
      if (n === nav) set({ library });
    } else if (where.page === "home") {
      const home = await t.home(where.key);
      if (n === nav) {
        const first = home.groups?.[0]?.rows?.[0]?.name ?? "";
        const selected = sameStack && s.selected ? s.selected : first;
        set({ home, selected });
        if (selected !== s.selected || !sameStack) await focus(where, selected);
      }
    } else if (where.page === "instance" && where.instance) {
      const instance = await t.instance(where.key, where.instance);
      if (n === nav) set({ instance });
    } else if (where.page === "empty" && where.dir) {
      const empty = await t.empty(where.dir);
      if (n === nav) set({ empty });
    }
  } catch (e) {
    t.log(`go ${where.page} failed: ${String(e)}`);
    if (n === nav) set({ error: String(e) });
  }
}

/** stackAction acts on a whole stack from the Library: up-last, up-all, up with names, down, forget. */
export async function stackAction(key: string, action: string, names: string[] = []) {
  const t = await transport();
  if (action.startsWith("up")) {
    // Up looks first (D19): with nothing in the way it just goes; with
    // something red, the checks' sheet asks.
    try {
      const p = await t.preflight(key, action, names);
      if (p.blocked) {
        set({ preflight: { key, action, names, p }, palette: false, bell: false });
        return;
      }
    } catch (e) {
      t.log(`preflight ${key}: ${String(e)}`);
    }
  }
  try {
    await t.stack(key, action, names);
  } catch (e) {
    t.log(`${action} ${key} failed: ${String(e)}`);
  }
}

/** preflightWay presses a way out of the checks' sheet. */
export async function preflightWay(f: view.Fix) {
  const pf = get().preflight;
  if (!pf) return;
  const t = await transport();
  try {
    switch (f.kind) {
      case "stop":
      case "without":
      case "anyway":
        set({ preflight: null });
        await t.preflightFix(pf.key, pf.action, pf.names, f.kind, f.pid ?? 0, f.verb ?? "");
        break;
      case "edit":
        t.open("file://" + (f.path ?? ""));
        break;
      case "repair":
        await repair();
        break;
      case "stack":
        set({ preflight: null });
        await askStack(pf.key, "remove");
        break;
    }
  } catch (e) {
    await say(String(e).replace(/^Error: /, ""), true);
  }
}

/** chooseStart opens a stopped stack's start sheet, on its page; "" closes it. */
export async function chooseStart(key: string) {
  if (key && get().where?.key !== key) await go({ key, page: "home" });
  set({ choosing: key });
}

/** setView remembers a stack's overview view (tiles, list, map); Go pushes the page back with it set. */
export async function setView(key: string, view: string) {
  set((s) => (s.home && s.home.key === key ? { home: { ...s.home, view } } : {}));
  const t = await transport();
  await t.setView(key, view);
}

/** dismissHints hides the Library's help strip for good. */
export async function dismissHints() {
  const t = await transport();
  await t.dismissHints();
  set((s) => (s.library ? { library: { ...s.library, steps: undefined } } : {}));
}

/** chooseFolder asks for a project folder; the app opens it, which lands on its tab or the empty page. */
export async function chooseFolder() {
  const t = await transport();
  await t.chooseFolder();
}

/** select picks a row on home; the dock follows it. */
export async function select(name: string) {
  const s = get();
  if (!s.where || s.where.page !== "home" || name === s.selected) return;
  set({ selected: name });
  await focus(s.where, name);
}

/** run sends a verb. A failure is said by Go as a toast, so here it is only logged. */
export async function run(key: string, verb: string, target: string) {
  const t = await transport();
  try {
    await t.do(key, verb, target);
  } catch (e) {
    t.log(`${verb} ${target} failed: ${String(e)}`);
  }
}

/** ask runs a verb on an instance, after the dash's confirmation when one is due. */
export async function ask(key: string, name: string, verb: string) {
  const t = await transport();
  const n = nav; // a dialog for a page that was left meanwhile is dropped
  try {
    const d = await t.dialog(key, name, verb);
    if (n !== nav) return;
    if (!d.needed) return run(key, verb, name);
    set({ dialog: { key, d }, palette: false, bell: false });
  } catch (e) {
    t.log(`${verb} ${name}: no dialog: ${String(e)}`);
  }
}

/** askStack runs a stack action (down, remove), asking first when Go says to: down when something in it is in use, remove always. */
export async function askStack(key: string, action: "down" | "remove") {
  const t = await transport();
  try {
    const d = await t.dialog(key, "", action);
    if (!d.needed) return stackAction(key, action);
    set({ dialog: { key, d }, palette: false, bell: false });
  } catch (e) {
    t.log(`${action} ${key}: no dialog: ${String(e)}`);
  }
}

/** askStop stops a stack, asking first when something in it is in use. */
export const askStop = (key: string) => askStack(key, "down");

/** choose presses a dialog button: its verb on its targets, in order, or a module action with what was typed. */
export async function choose(b: view.Button, text = "") {
  const s = get();
  const dialog = s.dialog;
  set({ dialog: null });
  if (!dialog || !b.verb) return;
  const t = await transport();
  // Removing the stack on screen leaves its page for the Library, where the row goes.
  if (dialog.d.stack && b.verb === "remove" && s.where?.key === dialog.key) void go({ key: "", page: "library" });
  try {
    if (dialog.d.stack) await t.stack(dialog.key, b.verb, b.targets ?? []);
    else if (dialog.d.setup) await t.setup(dialog.d.setup, b.verb, text);
    else if (dialog.d.action) await t.admin(dialog.key, dialog.d.instance ?? "", b.verb, b.targets?.[0] ?? "", text);
    else await t.doAll(dialog.key, b.verb, b.targets ?? []);
  } catch (e) {
    t.log(`${b.label} failed: ${String(e)}`);
  }
}

/** askAction runs a module's action on its row, after input or a question when the dash asked. */
export async function askAction(key: string, name: string, a: view.Action) {
  const t = await transport();
  try {
    const d = await t.action(key, name, a);
    if (!d.needed) {
      await t.admin(key, name, a.id, a.resource ?? "", "");
      return;
    }
    set({ dialog: { key, d }, palette: false, bell: false });
  } catch (e) {
    t.log(`${a.label} failed: ${String(e)}`);
  }
}

/** say tells Go something the page did, so it is toasted and kept in recent messages. */
export async function say(text: string, bad = false) {
  const t = await transport();
  await t.say(text, bad);
}

/** copy puts text on the clipboard and says so, as the dash's y did. */
export async function copy(text: string, what: string) {
  const t = await transport();
  try {
    await t.copy(text);
    await say(`copied ${what} to the clipboard`);
  } catch {
    await say(`could not copy ${what}`, true);
  }
}

let toastSeq = 0;
function toast(n: serve.Notice) {
  set((s) => ({ toasts: [...s.toasts, { id: ++toastSeq, text: n.text, bad: n.bad, shownAt: Date.now() }].slice(-5) }));
}

const themeKey = "doze.theme";
/** lastKey keeps the last page, for "reopen where I was"; reopenKey whether to. */
const lastKey = "doze.last";
const reopenKey = "doze.reopen";

/** reopens says whether the window reopens on the page it had; on unless turned off. */
export function reopens(): boolean {
  try {
    return localStorage.getItem(reopenKey) !== "off";
  } catch {
    return true;
  }
}

export function setReopens(on: boolean) {
  try {
    localStorage.setItem(reopenKey, on ? "on" : "off");
  } catch {
    // no storage: it lasts the window
  }
}

function lastPlace(): Where | null {
  try {
    const raw = localStorage.getItem(lastKey);
    return raw ? (JSON.parse(raw) as Where) : null;
  } catch {
    return null;
  }
}

const modeKey = "doze.mode";
const systemDark = window.matchMedia("(prefers-color-scheme: dark)");

/** paint draws the page in the theme and the mode, resolving system from macOS. */
function paint() {
  const { theme, mode } = get();
  const dark = mode === "system" ? systemDark.matches : mode === "dark";
  document.documentElement.dataset.theme = theme;
  document.documentElement.dataset.mode = dark ? "dark" : "light";
  if (get().dark !== dark) set({ dark });
}
// Following the system means following it while the window is open too.
systemDark.addEventListener("change", () => {
  if (get().mode === "system") paint();
});

function remember(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Private mode or a sandbox without storage: it lasts the window.
  }
}

/** setTheme sets the colour theme, keeps it, and tells Go. */
export async function setTheme(theme: Theme) {
  set({ theme });
  paint();
  remember(themeKey, theme);
  const t = await transport();
  await t.theme(theme);
}

/** setMode sets dark, light or system, keeps it, and tells Go. */
export async function setMode(mode: Mode) {
  set({ mode });
  paint();
  remember(modeKey, mode);
  const t = await transport();
  await t.mode(mode);
}

const themes: Theme[] = ["violet", "teal", "burgundy", "amber", "slate"];

/**
 * stored is the theme and mode last chosen. Before the two were apart one
 * value held both: "light" and "system" were modes on violet, and a colour
 * named alone was that colour, dark.
 */
function stored(): { theme: Theme; mode: Mode } {
  try {
    const t = localStorage.getItem(themeKey) ?? "";
    const m = localStorage.getItem(modeKey);
    if (m === "dark" || m === "light" || m === "system") return { theme: themes.includes(t as Theme) ? (t as Theme) : "violet", mode: m };
    if (t === "light") return { theme: "violet", mode: "light" };
    if (t === "dark") return { theme: "violet", mode: "dark" };
    if (themes.includes(t as Theme)) return { theme: t as Theme, mode: "dark" };
  } catch {
    // no storage
  }
  return { theme: "violet", mode: "system" };
}

/** useBoot connects the store to Go once: the stacks, the pushes, the first tab. */
export function useBoot() {
  useEffect(() => {
    let off: Array<() => void> = [];
    let gone = false;
    set(stored());
    paint();
    transport().then(async (t) => {
      if (gone) return;
      off = [
        t.on<serve.StackSummary[]>("stacks", (stacks) => set({ stacks: stacks ?? [] })),
        t.on<view.Library>("library", (library) => {
          if (get().where?.page === "library" || get().where?.page === "settings") set({ library });
        }),
        // A push is for the stack and page in focus; one computed for the
        // last focus, still in flight, is dropped.
        t.on<view.Home>("home", (home) => {
          const w = get().where;
          if (w?.page === "home" && home.key === w.key) set({ home });
        }),
        t.on<view.Instance>("instance", (instance) => {
          const w = get().where;
          if (w?.page === "instance" && instance.key === w.key && instance.name === w.instance) set({ instance });
        }),
        t.on<LogsChanged>("logs", (c) => set((s) => ({ logsVersion: s.logsVersion + 1, logsTotal: c.total }))),
        t.on<Nav>("nav", (n) => void go({ key: n.key, page: (n.page as Page) || "home", dir: n.dir, instance: n.instance })),
        t.on<serve.Notice>("notice", toast),
        t.on<view.Attention>("attention", gotAttention),
        // A native menu item: the same move as its key on the page.
        t.on<string>("menu", (item) => {
          const w = get().where;
          switch (item) {
            case "open":
              void chooseFolder();
              break;
            case "library":
              void go({ key: "", page: "library" });
              break;
            case "back":
              void back();
              break;
            case "forward":
              void forward();
              break;
            case "logs":
              if (w?.key) void go({ key: w.key, page: "logs" });
              break;
            case "bell":
              // The menu bar's "Open" and ⌘⇧N from the menu: open it, never close it.
              set({ bell: true, alerts: [], palette: false, help: false, away: null });
              break;
            case "palette":
              set((s) => ({ palette: !s.palette, dialog: null, help: false }));
              break;
            case "help":
              set((s) => ({ help: !s.help, palette: false }));
              break;
          }
        }),
      ];
      const [stacks, pending, version] = await Promise.all([t.stacks(), t.pending(), t.version()]);
      void t.attention().then(gotAttention);
      if (pending.bell) set({ bell: true });
      void t.away().then((away) => set({ away: away ?? null }));
      t.log(`stacks ${stacks.length}${pending.key ? ", opening the one asked for" : ""}`);
      set({ stacks, version });
      // The window opens on the Library, unless it was opened at a project.
      const asked = stacks.find((s) => s.key === pending.key);
      if (pending.page === "empty" && pending.dir) await go({ key: "", page: "empty", dir: pending.dir });
      else if (pending.page === "settings" || pending.page === "library") await go({ key: "", page: pending.page });
      else if (asked && pending.page === "instance" && pending.instance) await go({ key: asked.key, page: "instance", instance: pending.instance });
      else if (asked) await go({ key: asked.key, page: "home" });
      else if (!get().where) {
        // Where it was last time, when that stack is still here.
        const last = reopens() ? lastPlace() : null;
        if (last && (last.key === "" || stacks.some((s) => s.key === last.key))) await go(last);
        else await go({ key: "", page: "library" });
      }
      const onVisibility = () => {
        const s = get();
        if (document.hidden) void t.blur();
        else if (s.where) void focus(s.where, s.selected);
      };
      if (!gone) {
        document.addEventListener("visibilitychange", onVisibility);
        off.push(() => document.removeEventListener("visibilitychange", onVisibility));
      }
    });
    return () => {
      gone = true;
      for (const fn of off) fn();
    };
  }, []);
}

function samePlace(a: Where, b: Where): boolean {
  return a.key === b.key && a.page === b.page && (a.instance ?? "") === (b.instance ?? "") && (a.dir ?? "") === (b.dir ?? "");
}

/** back returns to the last place, as ⌘[ does; forward undoes a back. */
export async function back() {
  const s = get();
  const prev = s.history[s.history.length - 1];
  if (!prev) return;
  set({ history: s.history.slice(0, -1), future: s.where ? [s.where, ...s.future] : s.future });
  await go(prev, "back");
}

export async function forward() {
  const s = get();
  const next = s.future[0];
  if (!next) return;
  set({ future: s.future.slice(1), history: s.where ? [...s.history, s.where] : s.history });
  await go(next, "forward");
}

/**
 * upFrom is the page above: a service or the logs sit under their stack's
 * overview, a stack's overview and every other page under the Library.
 * The Library has nothing above it.
 */
export function upFrom(w: Where | null): Where | null {
  switch (w?.page) {
    case "instance":
    case "logs":
    case "recent":
    case "timeline":
      return { key: w.key, page: "home" };
    case "home":
    case "empty":
    case "settings":
      return { key: "", page: "library" };
  }
  return null;
}

/** up goes one level up, as esc does on every page. Settings is not a level but a place visited, so leaving it goes back to where it was opened from. */
export async function up() {
  const { where, history } = get();
  if (where?.page === "settings" && history.length > 0) {
    await back();
    return;
  }
  const w = upFrom(where);
  if (w) await go(w);
}

/** tabStacks are the stacks with a tab, in the strip's order: every running stack whose tab was not closed, and the one being looked at. ⌘1–9 count these. */
export function tabStacks(stacks: serve.StackSummary[], current: string, closed: string[] = []): serve.StackSummary[] {
  return stacks.filter((s) => s.key === current || (s.running && !closed.includes(s.key)));
}

/** repair runs the machine setup behind the administrator prompt; what happened is said by Go. */
export async function repair() {
  const t = await transport();
  try {
    await t.repair();
  } catch (e) {
    await say(String(e).replace(/^Error: /, ""), true);
  }
}
