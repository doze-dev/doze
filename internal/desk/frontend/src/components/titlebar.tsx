import { transport, type serve } from "../transport";
import { Bell } from "./bell";
import { closeTab, tabStacks, useStore, set } from "../store";

// The title bar, as every board draws it: the Library tab first, then a
// tab per open stack with its dot and a red count when something needs
// you, and the ⌘K button. There is no "+" tab: the Library, ⌘O and a
// dropped folder are how a stack arrives. Double-click zooms the window,
// as any title bar does; the strip itself is the drag region.
export function Titlebar({
  stacks,
  current,
  library,
  onLibrary,
  onPick,
  onCommand,
  pending,
}: {
  stacks: serve.StackSummary[];
  current: string;
  library: boolean;
  onLibrary: () => void;
  onPick: (key: string) => void;
  onCommand: () => void;
  pending?: { name: string; dir: string };
}) {
  // A tab is a running stack, or the one being looked at; the Library
  // lists the rest. A closed tab's stack keeps running.
  const closed = useStore((s) => s.closed);
  const open = tabStacks(stacks, library ? "" : current, closed);
  return (
    <header
      className="tb"
      onDoubleClick={(e) => {
        if ((e.target as HTMLElement).closest("button")) return;
        transport().then((t) => t.zoom());
      }}
    >
      <div className="stabs" role="tablist" aria-label="Tabs">
        <button role="tab" aria-selected={library} className={"stab" + (library ? " on" : "")} onClick={onLibrary} title="Home  ⌘0">
          <HomeIcon />
          <span className="t">Home</span>
        </button>
        {open.map((s, i) => (
          <span key={s.key} className={"stabw" + (!library && s.key === current ? " on" : "")}>
            <button
              role="tab"
              aria-selected={!library && s.key === current}
              className={"stab" + (!library && s.key === current ? " on" : "")}
              onClick={() => onPick(s.key)}
              title={`${s.dir}${s.broken ? " · doze.hcl does not load" : s.running && !s.live ? " · daemon not answering" : ""}  ⌘${i + 1}`}
            >
              <span className={"dot " + (s.trouble ? "r" : s.broken ? "y" : s.running ? (s.live ? "g" : "y") : "")} aria-hidden />
              <span className="t">{s.name || "stack"}</span>
              {s.trouble && <span className="badge">{s.troubled || "!"}</span>}
            </button>
            <button
              className="stabx"
              aria-label={`Close the ${s.name || "stack"} tab`}
              title={s.running ? "Close the tab. The stack keeps running; open it again from Home." : "Close the tab"}
              onClick={() => void closeTab(s.key)}
            >
              ×
            </button>
          </span>
        ))}
        {pending && (
          <button role="tab" aria-selected className="stab on" title={pending.dir}>
            <span className="dot" aria-hidden />
            <span className="t">{pending.name}</span>
          </button>
        )}
      </div>
      <Bell />
      <button className="bell" aria-label="Keyboard shortcuts" title="Keyboard shortcuts  ?  ⌘/" onClick={() => set((s) => ({ help: !s.help, palette: false, bell: false }))}>
        <span aria-hidden style={{ font: "600 13px var(--font-sans)" }}>?</span>
      </button>
      <button className="btn" title="Search a name, or type a verb and a name (⌘K)" onClick={onCommand}>
        <kbd>⌘K</kbd> Search or do
      </button>
    </header>
  );
}

/** HomeIcon is the first tab's mark: a house, drawn as the other glyphs are. */
function HomeIcon() {
  return (
    <svg width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" style={{ display: "block", flex: "none" }}>
      <path d="M2.5 7.2 8 2.5l5.5 4.7" />
      <path d="M4 6.2v7h3v-3.6h2v3.6h3v-7" />
    </svg>
  );
}
