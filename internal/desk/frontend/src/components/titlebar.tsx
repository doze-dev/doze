import { transport, type serve } from "../transport";

// The title bar (the boards' .titlebar.tabbed): one tab per stack (D11)
// with its dot, name and a red count when something needs you, the + for a
// new tab, and the ⌘K command button. Double-click zooms the window, as
// any title bar does; the strip itself is the drag region.
export function Titlebar({ stacks, current, onPick, onCommand }: { stacks: serve.StackSummary[]; current: string; onPick: (key: string) => void; onCommand: () => void }) {
  return (
    <header
      className="titlebar"
      onDoubleClick={(e) => {
        if ((e.target as HTMLElement).closest("button")) return;
        transport().then((t) => t.zoom());
      }}
    >
      <div className="stabs" role="tablist" aria-label="Stacks">
        {stacks.map((s, i) => (
          <button
            key={s.key}
            role="tab"
            aria-selected={s.key === current}
            className={"stab" + (s.key === current ? " on" : "")}
            onClick={() => onPick(s.key)}
            title={`${s.dir}  ⌘${i + 1}`}
          >
            <span className={"dot " + (s.trouble ? "r" : s.running ? (s.live ? "g" : "y") : "")} aria-hidden />
            {s.key === current ? <b>{s.name || "stack"}</b> : s.name || "stack"}
            {s.trouble && <span className="badge">!</span>}
          </button>
        ))}
        <button className="stab add" title="New tab (⌘T) — arrives with setup" disabled>
          +
        </button>
      </div>
      <div className="grow" />
      <button className="btn" title="Command palette" onClick={onCommand} disabled={stacks.length === 0}>
        <kbd>⌘K</kbd> Command
      </button>
    </header>
  );
}
