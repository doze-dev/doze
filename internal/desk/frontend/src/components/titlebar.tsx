import { transport, type serve } from "../transport";

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
  // A tab is a running stack, or the stopped one being looked at; the
  // Library lists the rest.
  const open = stacks.filter((s) => s.running || s.key === current);
  return (
    <header
      className="tb"
      onDoubleClick={(e) => {
        if ((e.target as HTMLElement).closest("button")) return;
        transport().then((t) => t.zoom());
      }}
    >
      <div className="stabs" role="tablist" aria-label="Tabs">
        <button role="tab" aria-selected={library} className={"stab" + (library ? " on" : "")} onClick={onLibrary} title="Library  ⌘0">
          <Bat />
          <span className="t">Library</span>
        </button>
        {open.map((s, i) => (
          <button
            key={s.key}
            role="tab"
            aria-selected={!library && s.key === current}
            className={"stab" + (!library && s.key === current ? " on" : "")}
            onClick={() => onPick(s.key)}
            title={`${s.dir}  ⌘${i + 1}`}
          >
            <span className={"dot " + (s.trouble ? "r" : s.running ? (s.live ? "g" : "y") : "")} aria-hidden />
            <span className="t">{s.name || "stack"}</span>
            {s.trouble && <span className="badge">{s.troubled || "!"}</span>}
          </button>
        ))}
        {pending && (
          <button role="tab" aria-selected className="stab on" title={pending.dir}>
            <span className="dot" aria-hidden />
            <span className="t">{pending.name}</span>
          </button>
        )}
      </div>
      <button className="btn" title="Search a name, or type a verb and a name (⌘K)" onClick={onCommand}>
        <kbd>⌘K</kbd> Search or do
      </button>
    </header>
  );
}

/** Bat is doze's mark, inline so the tab needs no request. */
export function Bat({ size = 14 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" aria-hidden="true" style={{ display: "block", flex: "none" }}>
      <path
        d="M8 3.2c.9 0 1.6.5 2 1.3.6-.9 1.8-1.5 3-1.5-.3 1.2-.2 2.4.4 3.3-.7 1.2-1.6 1.9-2.7 2.2-.3 1.3-1.2 2.3-2.7 3-1.5-.7-2.4-1.7-2.7-3-1.1-.3-2-1-2.7-2.2.6-.9.7-2.1.4-3.3 1.2 0 2.4.6 3 1.5.4-.8 1.1-1.3 2-1.3z"
        fill="currentColor"
      />
    </svg>
  );
}
