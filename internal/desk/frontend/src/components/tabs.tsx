import type { serve } from "../transport";

// One tab per stack (D11): a state dot, the name, and a red count when
// something needs you. The strip is the window's drag region.
export function Tabs({
  stacks,
  current,
  onPick,
}: {
  stacks: serve.StackSummary[];
  current: string;
  onPick: (key: string) => void;
}) {
  return (
    <header className="tabs" role="tablist" aria-label="Stacks">
      {stacks.map((s, i) => (
        <button
          key={s.key}
          role="tab"
          aria-selected={s.key === current}
          className={"tab" + (s.key === current ? " current" : "") + (s.running ? "" : " stopped")}
          onClick={() => onPick(s.key)}
          title={`${s.dir}  ⌘${i + 1}`}
        >
          <span className={"dot " + (s.trouble ? "bad" : s.running ? (s.live ? "good" : "warn") : "dim")} aria-hidden />
          <span className="name">{s.name || "stack"}</span>
          {s.running && s.awake > 0 && <span className="count">{s.awake}</span>}
          {s.trouble && <span className="needs" aria-label="needs you" />}
        </button>
      ))}
    </header>
  );
}
