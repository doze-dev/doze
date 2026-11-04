// The one door to Go. Inside the app it is the Wails bindings and events;
// in a browser (doze open --serve, Playwright, the Vite dev server) it is
// POST /api/<call> and server-sent events. Pages never know which.
import type * as view from "./bindings/github.com/doze-dev/doze/internal/desk/view/models";
import type * as serve from "./bindings/github.com/doze-dev/doze/internal/desk/serve/models";

export type { view, serve };

// Push payloads that no call returns, so the generator does not emit them;
// they mirror serve.LogsChanged and serve.Nav.
export interface LogsChanged {
  instance: string;
  total: number;
  version: number;
}
export interface Nav {
  key: string;
  page: string;
  /** dir is the folder an "empty" page is for: one with no doze.hcl yet. */
  dir?: string;
}

export interface Transport {
  stacks(): Promise<serve.StackSummary[]>;
  /** pending is the stack the app asked the page to open, once; an empty key means none. */
  pending(): Promise<Nav>;
  version(): Promise<string>;
  home(key: string): Promise<view.Home>;
  instance(key: string, name: string): Promise<view.Instance>;
  logs(key: string, q: view.LogsQuery): Promise<view.Logs>;
  focus(f: serve.Focus): Promise<void>;
  blur(): Promise<void>;
  do(key: string, verb: string, target: string): Promise<void>;
  /** doAll runs a verb on several targets in order, stopping at the first failure. */
  doAll(key: string, verb: string, targets: string[]): Promise<void>;
  /** dialog is the confirmation for a verb on an instance; needed is false when it should just be done. */
  dialog(key: string, name: string, verb: string): Promise<view.Dialog>;
  palette(key: string, selected: string, query: string): Promise<view.Palette>;
  /** console is an instance's console tab, resolved for the selected rows. */
  console(key: string, name: string, tab: string, sel: Record<string, number>): Promise<view.Console>;
  /** action is the confirmation for a module action; needed is false when it just runs. */
  action(key: string, name: string, a: view.Action): Promise<view.Dialog>;
  admin(key: string, name: string, action: string, resource: string, input: string): Promise<void>;
  recent(): Promise<serve.Notice[]>;
  /** say tells Go something the page did, so it is toasted and remembered. */
  say(text: string, bad: boolean): Promise<void>;
  theme(theme: string): Promise<void>;
  /** empty is the page for a folder with no doze.hcl yet. */
  empty(dir: string): Promise<view.Empty>;
  /** setup sets a folder up: init, add <arg>, or a starter file. */
  setup(dir: string, action: string, arg: string): Promise<void>;
  painted(ms: number): void;
  /** log sends a line to the app's stderr (a no-op in a browser). */
  log(line: string): void;
  /** zoom toggles the window between its size and the screen, as a double-click on a title bar does. */
  zoom(): void;
  /** on subscribes to a push by name; the returned function unsubscribes. */
  on<T>(event: string, fn: (data: T) => void): () => void;
}

/** inWails says the page runs inside the app's web view. */
export function inWails(): boolean {
  return typeof window !== "undefined" && (window as unknown as { _wails?: unknown })._wails !== undefined;
}

async function wails(): Promise<Transport> {
  const [{ Events, Window }, api] = await Promise.all([
    import("@wailsio/runtime"),
    import("./bindings/github.com/doze-dev/doze/internal/desk/app/api"),
  ]);
  return {
    stacks: () => api.Stacks().then((s) => s ?? []),
    pending: () => api.Pending(),
    version: () => api.Version(),
    home: (key) => api.Home(key),
    instance: (key, name) => api.Instance(key, name),
    logs: (key, q) => api.Logs(key, q),
    focus: (f) => api.Focus(f),
    blur: () => api.Blur(),
    do: (key, verb, target) => api.Do(key, verb, target),
    doAll: (key, verb, targets) => api.DoAll(key, verb, targets),
    dialog: (key, name, verb) => api.Dialog(key, name, verb),
    palette: (key, selected, query) => api.Palette(key, selected, query),
    console: (key, name, tab, sel) => api.Console(key, name, tab, sel),
    action: (key, name, a) => api.Action(key, name, a),
    admin: (key, name, action, resource, input) => api.Admin(key, name, action, resource, input),
    recent: () => api.Recent().then((r) => r ?? []),
    say: (text, bad) => api.Say(text, bad),
    theme: (theme) => api.Theme(theme),
    empty: (dir) => api.Empty(dir),
    setup: (dir, action, arg) => api.Setup(dir, action, arg),
    painted: (ms) => void api.Painted(ms),
    log: (line) => void api.Log(line),
    zoom: () => void Window.ToggleMaximise(),
    on: (event, fn) => Events.On(event, (e: { data: unknown }) => fn(e.data as never)),
  };
}

function http(): Transport {
  const call = async <T>(name: string, body: unknown): Promise<T> => {
    const r = await fetch(`/api/${name}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body ?? {}),
    });
    const out = await r.json();
    if (!r.ok) throw new Error(out?.error ?? r.statusText);
    return out as T;
  };
  let source: EventSource | null = null;
  const listeners = new Map<string, Set<(data: unknown) => void>>();
  const attached = new Set<string>();
  // attach listens to one event name on the stream, once per name.
  const attach = (name: string) => {
    if (!source || attached.has(name)) return;
    attached.add(name);
    source.addEventListener(name, (e) => {
      const data = JSON.parse((e as MessageEvent).data);
      if (name === "notice" || name === "nav" || name === "console") console.log("push " + name, data?.tab ?? data?.text ?? "");
      for (const fn of listeners.get(name) ?? []) fn(data);
    });
  };
  const ensure = () => {
    if (!source) source = new EventSource("/events");
    for (const name of listeners.keys()) attach(name);
  };
  return {
    stacks: () => call("stacks", {}),
    pending: () => call("pending", {}),
    version: () => call("version", {}),
    home: (key) => call("home", { key }),
    instance: (key, name) => call("instance", { key, name }),
    logs: (key, query) => call("logs", { key, query }),
    focus: (f) => call("focus", f),
    blur: () => call("focus", { key: "", page: "" }),
    do: (key, verb, target) => call("do", { key, verb, target }),
    doAll: (key, verb, targets) => call("doall", { key, verb, targets }),
    dialog: (key, name, verb) => call("dialog", { key, name, verb }),
    palette: (key, selected, query) => call("palette", { key, selected, query }),
    console: (key, name, tab, sel) => call("console", { key, name, tab, sel }),
    action: (key, name, a) => call("action", { key, name, action: a }),
    admin: (key, name, action, resource, input) => call("admin", { key, name, action, resource, input }),
    recent: () => call<serve.Notice[] | null>("recent", {}).then((r) => r ?? []),
    say: (text, bad) => call("say", { text, bad }),
    theme: (theme) => call("theme", { theme }),
    empty: (dir) => call("empty", { dir }),
    setup: (dir, action, arg) => call("setup", { dir, action, arg }),
    painted: () => {},
    log: (line) => console.log(line),
    zoom: () => {},
    on: (event, fn) => {
      const set = listeners.get(event) ?? new Set();
      set.add(fn as (data: unknown) => void);
      listeners.set(event, set);
      ensure();
      return () => {
        set.delete(fn as (data: unknown) => void);
      };
    },
  };
}

let chosen: Promise<Transport> | null = null;

/** transport is the page's transport, chosen once. */
export function transport(): Promise<Transport> {
  if (!chosen) {
    chosen = (inWails() ? wails() : Promise.resolve(http())).then((t) => {
      t.log(`transport ${inWails() ? "wails" : "http"}`);
      window.addEventListener("error", (e) => t.log(`error ${e.message} at ${e.filename}:${e.lineno}`));
      window.addEventListener("unhandledrejection", (e) => t.log(`rejection ${String((e as PromiseRejectionEvent).reason)}`));
      return t;
    });
  }
  return chosen;
}
