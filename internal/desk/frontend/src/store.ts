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
  where: Where | null;
  home: view.Home | null;
  instance: view.Instance | null;
  /** logsVersion bumps when the focused logs changed; the logs page refetches. */
  logsVersion: number;
  logsTotal: number;
  error: string | null;
}

let state: State = { stacks: [], where: null, home: null, instance: null, logsVersion: 0, logsTotal: 0, error: null };
const subs = new Set<() => void>();

function set(patch: Partial<State>) {
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

/** go moves the page; Go follows with its pushes. */
export async function go(where: Where) {
  const t = await transport();
  set({ where, home: where.page === "home" && state.where?.key === where.key ? state.home : null, instance: null, error: null });
  await t.focus({ key: where.key, page: where.page, instance: where.instance ?? "" });
  // The first paint does not wait for a push.
  try {
    if (where.page === "home") set({ home: await t.home(where.key) });
    else if (where.page === "instance" && where.instance) set({ instance: await t.instance(where.key, where.instance) });
  } catch (e) {
    set({ error: String(e) });
  }
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
        t.on<view.Home>("home", (home) => {
          if (state.where?.page === "home" && home.stack === state.home?.stack) set({ home });
          else if (state.where?.page === "home") set({ home });
        }),
        t.on<view.Instance>("instance", (instance) => {
          if (state.where?.page === "instance" && instance.name === state.where.instance) set({ instance });
        }),
        t.on<LogsChanged>("logs", (c) => set({ logsVersion: state.logsVersion + 1, logsTotal: c.total })),
        t.on<Nav>("nav", (n) => void go({ key: n.key, page: (n.page as Page) || "home" })),
      ];
      const stacks = await t.stacks();
      set({ stacks });
      if (!state.where && stacks.length) await go({ key: stacks[0].key, page: "home" });
      const onVisibility = () => {
        if (document.hidden) void t.blur();
        else if (state.where) void go(state.where);
      };
      document.addEventListener("visibilitychange", onVisibility);
      off.push(() => document.removeEventListener("visibilitychange", onVisibility));
    });
    return () => {
      gone = true;
      for (const fn of off) fn();
    };
  }, []);
}
