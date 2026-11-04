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
}
export interface Nav {
  key: string;
  page: string;
}

export interface Transport {
  stacks(): Promise<serve.StackSummary[]>;
  /** pending is the stack the app asked the page to open, once; an empty key means none. */
  pending(): Promise<Nav>;
  home(key: string): Promise<view.Home>;
  instance(key: string, name: string): Promise<view.Instance>;
  logs(key: string, q: view.LogsQuery): Promise<view.Logs>;
  focus(f: serve.Focus): Promise<void>;
  blur(): Promise<void>;
  do(key: string, verb: string, target: string): Promise<void>;
  painted(ms: number): void;
  /** log sends a line to the app's stderr (a no-op in a browser). */
  log(line: string): void;
  /** on subscribes to a push by name; the returned function unsubscribes. */
  on<T>(event: string, fn: (data: T) => void): () => void;
}

/** inWails says the page runs inside the app's web view. */
export function inWails(): boolean {
  return typeof window !== "undefined" && (window as unknown as { _wails?: unknown })._wails !== undefined;
}

async function wails(): Promise<Transport> {
  const [{ Events }, api] = await Promise.all([
    import("@wailsio/runtime"),
    import("./bindings/github.com/doze-dev/doze/internal/desk/app/api"),
  ]);
  return {
    stacks: () => api.Stacks().then((s) => s ?? []),
    pending: () => api.Pending(),
    home: (key) => api.Home(key),
    instance: (key, name) => api.Instance(key, name),
    logs: (key, q) => api.Logs(key, q),
    focus: (f) => api.Focus(f),
    blur: () => api.Blur(),
    do: (key, verb, target) => api.Do(key, verb, target),
    painted: (ms) => void api.Painted(ms),
    log: (line) => void api.Log(line),
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
  const ensure = () => {
    if (source) return;
    source = new EventSource("/events");
    for (const name of listeners.keys()) attach(name);
  };
  const attach = (name: string) => {
    source?.addEventListener(name, (e) => {
      const data = JSON.parse((e as MessageEvent).data);
      for (const fn of listeners.get(name) ?? []) fn(data);
    });
  };
  return {
    stacks: () => call("stacks", {}),
    pending: () => call("pending", {}),
    home: (key) => call("home", { key }),
    instance: (key, name) => call("instance", { key, name }),
    logs: (key, query) => call("logs", { key, query }),
    focus: (f) => call("focus", f),
    blur: () => call("focus", { key: "", page: "" }),
    do: (key, verb, target) => call("do", { key, verb, target }),
    painted: () => {},
    log: (line) => console.log(line),
    on: (event, fn) => {
      const set = listeners.get(event) ?? new Set();
      const had = listeners.has(event);
      set.add(fn as (data: unknown) => void);
      listeners.set(event, set);
      ensure();
      if (!had && source) attach(event);
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
