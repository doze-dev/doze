import { useEffect } from "react";

// The Help sheet, as the Help board draws it: the TUI's keys, kept, plus
// the window's own, in four columns, and what the meters mean. Only keys
// that work today are listed; the words are the dash's keymap.

type Key = [string, string, string?];

const HOME: Key[] = [
  ["↑↓", "move"],
  ["⏎", "open"],
  ["w", "wake", "start it now, and what it needs"],
  ["s", "sleep", "asks first when something is using it"],
  ["r", "restart", "rerun, for a task"],
  ["l", "logs"],
  ["L", "all logs"],
  ["y", "copy address"],
];
const PAGE: Key[] = [
  ["tab", "next tab"],
  ["p", "pin awake", "modules"],
  ["r", "run now", "a task"],
  ["↑↓", "select", "in a module's console; its action keys are shown"],
];
const LOGS: Key[] = [
  ["f", "level", "debug, info, warnings, errors only"],
  ["/", "search"],
  ["t", "time", "clock or −3m 05s"],
  ["[ ]", "earlier runs"],
];
const EVERYWHERE: Key[] = [
  ["⌘K", "search and do anything", "was : in the TUI"],
  ["⌘1–9", "switch stack"],
  ["⌘L", "all logs"],
  ["?", "this sheet"],
  ["esc", "back"],
];
const MOUSE: Key[] = [
  ["click", "select a row"],
  ["dbl-click", "open it"],
  ["dbl-click", "the title bar zooms the window"],
];

export function Help({ version, onClose }: { version: string; onClose: () => void }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      e.stopPropagation();
      if (e.key === "Escape" || e.key === "?") {
        e.preventDefault();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [onClose]);
  const rows = (keys: Key[]) =>
    keys.map(([k, what, more], i) => (
      <div key={k + i} className="kr">
        <kbd>{k}</kbd>
        <span>
          {what}
          {more && <small>{more}</small>}
        </span>
      </div>
    ));
  return (
    <div className="scrim" onClick={onClose}>
      <section className="sheet" role="dialog" aria-label="Keyboard shortcuts" onClick={(e) => e.stopPropagation()}>
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <span className="acc" style={{ fontSize: 18 }} aria-hidden>
            z
          </span>
          <h2>Keys</h2>
          <span className="dim">doze {version} · the TUI's keys, kept, plus the window's own</span>
          <span className="grow" />
          <button className="btn" onClick={onClose}>
            Close <kbd>esc</kbd>
          </button>
        </div>
        <div className="cols">
          <div>
            <h3>Home</h3>
            {rows(HOME)}
          </div>
          <div>
            <h3>This page</h3>
            {rows(PAGE)}
            <h3>Logs</h3>
            {rows(LOGS)}
          </div>
          <div>
            <h3>Everywhere</h3>
            {rows(EVERYWHERE)}
          </div>
          <div>
            <h3>Mouse</h3>
            {rows(MOUSE)}
          </div>
        </div>
        <div className="meters">
          <div>
            <b>mem</b> is memory in use, cut by instance. ≈ resident is the stack's total.
          </div>
          <div>
            <b>cpu</b> is cores in use: 1 core is one full CPU, so a busy stack can show 2.5 cores.
          </div>
          <div>
            <b>charts</b> take a reading a second; "last 4m" is how much the daemon has kept.
          </div>
        </div>
      </section>
    </div>
  );
}
