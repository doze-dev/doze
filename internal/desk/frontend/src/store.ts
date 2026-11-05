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
}

export type Theme = "system" | "violet" | "teal" | "burgundy" | "amber" | "slate" | "light" | "dark";

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
  /** history is where the page has been, oldest first; future what back left behind. ⌘[ and ⌘] walk them. */
  history: Where[];
  future: Where[];
  /** tab is the instance page's tab; sel the selected row of each console table, by block id. Go follows both. */
  tab: string;
  sel: Record<string, number>;
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
  show: null,
  logsVersion: 0,
  logsTotal: 0,
  error: null,
  dialog: null,
  palette: false,
  help: false,
  toasts: [],
  theme: "system",
  history: [],
  future: [],
  tab: "",
  sel: {},
});

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
  try {
    await t.stack(key, action, names);
  } catch (e) {
    t.log(`${action} ${key} failed: ${String(e)}`);
  }
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
    set({ dialog: { key, d }, palette: false });
  } catch (e) {
    t.log(`${verb} ${name}: no dialog: ${String(e)}`);
  }
}

/** choose presses a dialog button: its verb on its targets, in order, or a module action with what was typed. */
export async function choose(b: view.Button, text = "") {
  const s = get();
  const dialog = s.dialog;
  set({ dialog: null });
  if (!dialog || !b.verb) return;
  const t = await transport();
  try {
    if (dialog.d.setup) await t.setup(dialog.d.setup, b.verb, text);
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
    set({ dialog: { key, d }, palette: false });
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
    await say(`copied  to the clipboard`);
  } catch {
    await say(`could not copy ${what}`, true);
  }
}

let toastSeq = 0;
function toast(n: serve.Notice) {
  set((s) => ({ toasts: [...s.toasts, { id: ++toastSeq, text: n.text, bad: n.bad, shownAt: Date.now() }].slice(-5) }));
}

const themeKey = "doze.theme";

/** setTheme applies a theme to the page, remembers it, and tells Go. */
export async function setTheme(theme: Theme) {
  set({ theme });
  if (theme === "system") delete document.documentElement.dataset.theme;
  else document.documentElement.dataset.theme = theme;
  try {
    localStorage.setItem(themeKey, theme);
  } catch {
    // Private mode or a sandbox without storage: the theme lasts the window.
  }
  const t = await transport();
  await t.theme(theme);
}

function storedTheme(): Theme {
  try {
    const v = localStorage.getItem(themeKey);
    if (v === "dark") return "violet";
    if (v && v !== "system") return v as Theme;
  } catch {
    // no storage
  }
  return "system";
}

/** useBoot connects the store to Go once: the stacks, the pushes, the first tab. */
export function useBoot() {
  useEffect(() => {
    let off: Array<() => void> = [];
    let gone = false;
    void setTheme(storedTheme());
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
      ];
      const [stacks, pending, version] = await Promise.all([t.stacks(), t.pending(), t.version()]);
      t.log(`stacks ${stacks.length}${pending.key ? ", opening the one asked for" : ""}`);
      set({ stacks, version });
      // The window opens on the Library, unless it was opened at a project.
      const asked = stacks.find((s) => s.key === pending.key);
      if (pending.page === "empty" && pending.dir) await go({ key: "", page: "empty", dir: pending.dir });
      else if (pending.page === "settings") await go({ key: "", page: "settings" });
      else if (asked && pending.page === "instance" && pending.instance) await go({ key: asked.key, page: "instance", instance: pending.instance });
      else if (asked) await go({ key: asked.key, page: "home" });
      else if (!get().where) await go({ key: "", page: "library" });
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

/** up goes one level up, as esc does on every page. */
export async function up() {
  const w = upFrom(get().where);
  if (w) await go(w);
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
