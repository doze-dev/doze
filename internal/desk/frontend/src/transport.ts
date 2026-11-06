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
  /** instance is set when a notification's Show asked for an instance's page. */
  instance?: string;
  /** bell says Needs you was asked for as the window opened. */
  bell?: boolean;
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
  /** copy puts text on the clipboard: Go's in the app, the browser's under --serve. */
  copy(text: string): Promise<void>;
  /** repair runs the machine setup behind the administrator prompt. */
  repair(): Promise<void>;
  /** openAtLogin starts the app at login, in the menu bar only, or stops. */
  openAtLogin(on: boolean): Promise<void>;
  /** stopHolder stops what holds a crashed service's port, then restarts it. */
  stopHolder(key: string, name: string, pid: number): Promise<void>;
  /** timeline is a stack's last half hour. */
  timeline(key: string): Promise<view.Timeline>;
  theme(theme: string): Promise<void>;
  mode(mode: string): Promise<void>;
  dismiss(key: string): Promise<void>;
  /** empty is the page for a folder with no doze.hcl yet. */
  empty(dir: string): Promise<view.Empty>;
  /** setup sets a folder up: init, add <arg>, or a starter file. */
  setup(dir: string, action: string, arg: string): Promise<void>;
  /** library is the first tab: every stack, the help strip, the machine line. */
  library(): Promise<view.Library>;
  /** stack acts on a whole stack: up-last, up-all, up with names, down, remove. */
  stack(key: string, action: string, names: string[]): Promise<void>;
  /** leftovers are what stacks left behind: stray engines and daemons, state with no stack, stale routes. */
  leftovers(): Promise<view.Leftover[]>;
  /** clearLeftover clears one by its id; "all" clears every one that deletes no data. */
  clearLeftover(id: string): Promise<void>;
  /** attention is Needs you: what is wrong now, everywhere, each with its fix. */
  attention(): Promise<view.Attention>;
  /** away is what happened while the window was closed, once; null for nothing. */
  away(): Promise<view.Away | null>;
  /** preflight looks before a stopped stack starts; blocked says to ask first. */
  preflight(key: string, action: string, names: string[]): Promise<view.Preflight>;
  /** preflightInstance looks before one service of a running stack starts. */
  preflightInstance(key: string, name: string): Promise<view.Preflight>;
  /** preflightInstanceFix is a way out for one service: stop (pid), then the verb, or anyway. */
  preflightInstanceFix(key: string, name: string, verb: string, kind: string, pid: number): Promise<void>;
  /** preflightFix is a way out from the checks: stop (pid), without (a name), anyway. */
  preflightFix(key: string, action: string, names: string[], kind: string, pid: number, without: string): Promise<void>;
  /** notNow puts an item aside until what it says changes; "" brings it back. */
  notNow(id: string, print: string): Promise<void>;
  /** dismissHints hides the Library's help strip for good. */
  dismissHints(): Promise<void>;
  /** chooseFolder shows the folder picker and opens what was chosen; "" when cancelled. */
  chooseFolder(): Promise<string>;
  /** setView remembers a stack's overview view: tiles, list or map. */
  setView(key: string, view: string): Promise<void>;
  painted(ms: number): void;
  /** log sends a line to the app's stderr (a no-op in a browser). */
  log(line: string): void;
  /** zoom toggles the window between its size and the screen, as a double-click on a title bar does. */
  zoom(): void;
  /** open opens a URL in the person's browser. */
  open(url: string): void;
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
    copy: (text) => api.Copy(text),
    repair: () => api.Repair(),
    openAtLogin: (on) => api.SetOpenAtLogin(on),
    stopHolder: (key, name, pid) => api.StopHolder(key, name, pid),
    timeline: (key) => api.Timeline(key),
    theme: (theme) => api.Theme(theme),
    mode: (mode) => api.Mode(mode),
    dismiss: (key) => api.Dismiss(key),
    empty: (dir) => api.Empty(dir),
    setup: (dir, action, arg) => api.Setup(dir, action, arg),
    library: () => api.Library(),
    stack: (key, action, names) => api.Stack(key, action, names),
    leftovers: () => api.Leftovers().then((l) => l ?? []),
    attention: () => api.Attention(),
    away: () => api.Away(),
    preflight: (key, action, names) => api.Preflight(key, action, names),
    preflightInstance: (key, name) => api.PreflightInstance(key, name),
    preflightInstanceFix: (key, name, verb, kind, pid) => api.PreflightInstanceFix(key, name, verb, kind, pid),
    preflightFix: (key, action, names, kind, pid, without) => api.PreflightFix(key, action, names, kind, pid, without),
    notNow: (id, print) => api.NotNow(id, print),
    clearLeftover: (id) => api.ClearLeftover(id),
    dismissHints: () => api.DismissHints(),
    chooseFolder: () => api.ChooseFolder(),
    setView: (key, view) => api.SetView(key, view),
    painted: (ms) => void api.Painted(ms),
    log: (line) => void api.Log(line),
    zoom: () => void Window.ToggleMaximise(),
    open: (url) => void api.Open(url),
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
    copy: (text) => navigator.clipboard.writeText(text),
    repair: () => call("repair", {}),
    openAtLogin: (on) => call("openatlogin", { on }),
    stopHolder: (key, name, pid) => call("stopholder", { key, name, pid }),
    timeline: (key) => call("timeline", { key }),
    theme: (theme) => call("theme", { theme }),
    mode: (mode) => call("mode", { mode }),
    dismiss: (key) => call("dismiss", { key }),
    empty: (dir) => call("empty", { dir }),
    setup: (dir, action, arg) => call("setup", { dir, action, arg }),
    library: () => call("library", {}),
    stack: (key, action, names) => call("stack", { key, action, names }),
    leftovers: () => call<view.Leftover[] | null>("leftovers", {}).then((l) => l ?? []),
    attention: () => call("attention", {}),
    away: () => call("away", {}),
    preflight: (key, action, names) => call("preflight", { key, action, names }),
    preflightInstance: (key, name) => call("preflightinstance", { key, name }),
    preflightInstanceFix: (key, name, verb, kind, pid) => call("preflightinstancefix", { key, name, verb, kind, pid }),
    preflightFix: (key, action, names, kind, pid, without) => call("preflightfix", { key, action, names, kind, pid, without }),
    notNow: (id, print) => call("notnow", { id, print }),
    clearLeftover: (id) => call("clearleftover", { id }),
    dismissHints: () => call("dismisshints", {}),
    setView: (key, view) => call("setview", { key, view }),
    chooseFolder: async () => {
      // No native picker in a browser: a path typed in is opened the same way.
      const dir = window.prompt("Project folder") ?? "";
      if (dir) await call("open", { dir });
      return dir;
    },
    painted: () => {},
    log: (line) => console.log(line),
    zoom: () => {},
    open: (url) => void window.open(url, "_blank", "noopener"),
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
