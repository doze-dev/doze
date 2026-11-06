import { setDensity, useStore, type Density } from "../store";

// How much room a list's rows get, as Gmail offers it: compact, cosy, or
// roomy (the default). One choice for every list; kept on this Mac.
const CHOICES: { d: Density; label: string; gap: number }[] = [
  { d: "compact", label: "Compact", gap: 3 },
  { d: "cosy", label: "Cosy", gap: 4.5 },
  { d: "roomy", label: "Roomy", gap: 6 },
];

/** Lines is three bars as far apart as the rows would be. */
function Lines({ gap }: { gap: number }) {
  const top = 8 - gap;
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
      {[0, 1, 2].map((i) => (
        <rect key={i} x="2" y={top + i * gap - 0.75} width="12" height="1.5" rx="0.75" fill="currentColor" />
      ))}
    </svg>
  );
}

/** DensityPick is the choice; words shows the names beside the marks (Settings), else marks alone (a list's head). */
export function DensityPick({ words }: { words?: boolean }) {
  const density = useStore((s) => s.density);
  return (
    <div className={"seg" + (words ? "" : " icons")} role="radiogroup" aria-label="Row density" style={words ? { alignSelf: "flex-start" } : undefined}>
      {CHOICES.map((c) => (
        <button key={c.d} role="radio" aria-checked={density === c.d} aria-label={c.label} title={`${c.label} rows`} className={density === c.d ? "on" : ""} onClick={() => setDensity(c.d)}>
          <Lines gap={c.gap} />
          {words && c.label}
        </button>
      ))}
    </div>
  );
}
