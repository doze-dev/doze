import { useEffect, useRef } from "react";
import { preflightWay, set, useStore } from "../store";

// The checks before Up, when something is in the way (the PreFlight
// board): each check with its ways out, and nothing started yet. With
// nothing red this sheet never shows.
export function PreflightSheet() {
  const pf = useStore((s) => s.preflight);
  const ref = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!pf) return;
    ref.current?.querySelector<HTMLButtonElement>(".btn.pri")?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        set({ preflight: null });
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [pf]);
  if (!pf) return null;
  const bad = (pf.p.checks ?? []).filter((c) => c.tone === "bad").length;
  return (
    <div className="scrim" onMouseDown={(e) => e.target === e.currentTarget && set({ preflight: null })}>
      <section ref={ref} className="dialog pf" role="dialog" aria-label={`Before ${pf.p.stack} starts`}>
        <h2>
          Before {pf.p.stack} starts: {bad === 1 ? "one thing is" : `${bad} things are`} in the way
        </h2>
        <p className="lead">doze looks before every Up. When all is well there is no sheet: the switch flips and the stack starts.</p>
        {(pf.p.checks ?? []).map((c, i) => (
          <div key={i} className={"ck" + (c.tone === "ok" ? " okr" : "")}>
            <span className={"g " + c.tone} aria-hidden>
              {c.tone === "ok" ? "✓" : c.tone === "bad" ? "✕" : "!"}
            </span>
            <div className="t">
              <b>{c.title}</b>
              {c.detail && <span className={c.mono ? "mono" : undefined}>{c.detail}</span>}
              {c.note && <span>{c.note}</span>}
              {(c.fixes ?? []).length > 0 && (
                <div className="fx">
                  {(c.fixes ?? []).map((f, j) => (
                    <button key={j} className={"btn sm" + (f.primary ? " pri" : f.kind === "edit" ? " quiet" : "")} onClick={() => void preflightWay(f)}>
                      {f.label}
                    </button>
                  ))}
                </div>
              )}
            </div>
          </div>
        ))}
        <div className="acts">
          <span className="note">nothing has started yet</span>
          <button className="btn quiet" onClick={() => void preflightWay({ label: "Up anyway", kind: "anyway" })} title="Start as asked; what is in the way will show as it happens">
            Up anyway
          </button>
          <button className="btn" onClick={() => set({ preflight: null })}>
            Cancel <kbd>esc</kbd>
          </button>
        </div>
      </section>
    </div>
  );
}
