// What the page holds: the stacks for the tab strip, where the user is, and
// the view in focus, each replaced whole by Go's pushes. Go computes every
// fact (D16); this only keeps the latest and tells React.
import { useEffect, useSyncExternalStore } from "react";
import { transport, type LogsChanged, type Nav, type serve, type view } from "./transport";

export type Page = "home" | "instance" | "logs";

export interface Where {
  key: string;
  page: Page;
  instance?: string;
}

interface State {
  stacks: serve.StackSummary[];
  version: string;
  where: Where | null;
  home: view.Home | null;
  instance: view.Instance | null;
  /** selected is the row the home table has picked; its logs fill the dock. */
  selected: string;
  /** show, on the merged logs page, is the instances ticked in the sidebar; null is all. */
  show: string[] | null;
  /** logsVersion bumps when the focused logs changed; the logs views refetch. */
  logsVersion: number;
  logsTotal: number;
  error: string | null;
}

let state: State = {
  stacks: [],
  version: "",
  where: null,
  home: null,
  instance: null,
  selected: "",
  show: null,
  logsVersion: 0,
  logsTotal: 0,
  error: null,
};
const subs = new Set<() => void>();

export function set(patch: Partial<State>) {
  state = { ...state, ...patch };
  for (const fn of subs) fn();
}

export function useStore(): State {
  return useSyncExternalStore(
    (fn) => {
      subs.add(fn);
      return () => subs.delete(fn);
    },
    () => state,
  );
}

// nav numbers each navigation, so a slow answer for the last page does not
// land on the next one.
let nav = 0;

/** focus tells Go what is on screen: the page, and on home the selected row. */
async function focus(where: Where, selected: string) {
  const t = await transport();
  await t.focus({ key: where.key, page: where.page, instance: where.page === "home" ? selected : (where.instance ?? "") });
}

/** go moves the page; Go follows with its pushes. */
export async function go(where: Where) {
  const n = ++nav;
  const t = await transport();
  const sameStack = state.where?.key === where.key;
  set({ where, home: sameStack ? state.home : null, instance: null, error: null, show: sameStack ? state.show : null });
  await focus(where, sameStack ? state.selected : "");
  // The first paint does not wait for a push.
  try {
    if (where.page === "home") {
      const home = await t.home(where.key);
      if (n === nav) {
        const first = home.groups?.[0]?.rows?.[0]?.name ?? "";
        const selected = sameStack && state.selected ? state.selected : first;
        set({ home, selected });
        if (selected !== state.selected || !sameStack) await focus(where, selected);
      }
    } else if (where.page === "instance" && where.instance) {
      const instance = await t.instance(where.key, where.instance);
      if (n === nav) set({ instance });
    }
  } catch (e) {
    t.log(`go ${where.page} failed: ${String(e)}`);
    if (n === nav) set({ error: String(e) });
  }
}

/** select picks a row on home; the dock follows it. */
export async function select(name: string) {
  if (!state.where || state.where.page !== "home" || name === state.selected) return;
  set({ selected: name });
  await focus(state.where, name);
}

/** run sends a verb; a failure is shown, not thrown. */
export async function run(key: string, verb: string, target: string) {
  const t = await transport();
  try {
    await t.do(key, verb, target);
    set({ error: null });
  } catch (e) {
    set({ error: (e as Error).message ?? String(e) });
  }
}

/** useBoot connects the store to Go once: the stacks, the pushes, the first tab. */
export function useBoot() {
  useEffect(() => {
    let off: Array<() => void> = [];
    let gone = false;
    transport().then(async (t) => {
      if (gone) return;
      off = [
        t.on<serve.StackSummary[]>("stacks", (stacks) => {
          set({ stacks: stacks ?? [] });
          if (!state.where && stacks?.length) void go({ key: stacks[0].key, page: "home" });
        }),
        // A push is for the stack and page in focus; one computed for the
        // last focus, still in flight, is dropped.
        t.on<view.Home>("home", (home) => {
          if (state.where?.page === "home" && home.key === state.where.key) set({ home });
        }),
        t.on<view.Instance>("instance", (instance) => {
          if (state.where?.page === "instance" && instance.key === state.where.key && instance.name === state.where.instance) set({ instance });
        }),
        t.on<LogsChanged>("logs", (c) => set({ logsVersion: state.logsVersion + 1, logsTotal: c.total })),
        t.on<Nav>("nav", (n) => void go({ key: n.key, page: (n.page as Page) || "home" })),
      ];
      const [stacks, pending, version] = await Promise.all([t.stacks(), t.pending(), t.version()]);
      t.log(`stacks ${stacks.length}${pending.key ? ", opening the one asked for" : ""}`);
      set({ stacks, version });
      const first = stacks.find((s) => s.key === pending.key) ?? stacks[0];
      if (!state.where && first) await go({ key: first.key, page: "home" });
      const onVisibility = () => {
        if (document.hidden) void t.blur();
        else if (state.where) void focus(state.where, state.selected);
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
