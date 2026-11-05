import { useEffect, useState } from "react";
import { set, useStore } from "../store";

const GOOD_MS = 4000;
const BAD_MS = 12000;

// Toasts at the bottom right, as the Notices board draws them: a success
// fades after 4 s, a failure stays 12 s or until dismissed, as in the TUI.
// The right edge counts down, as the board's does.
export function Toasts() {
  const toasts = useStore((s) => s.toasts);
  const [, setTick] = useState(0);
  useEffect(() => {
    if (toasts.length === 0) return;
    const tick = setInterval(() => {
      const t = Date.now();
      setTick(t);
      set((s) => ({ toasts: s.toasts.filter((x) => x.shownAt + (x.bad ? BAD_MS : GOOD_MS) > t) }));
    }, 1000);
    return () => clearInterval(tick);
  }, [toasts.length]);
  if (toasts.length === 0) return null;
  const dismiss = (id: number) => set((s) => ({ toasts: s.toasts.filter((x) => x.id !== id) }));
  return (
    <div className="toasts" aria-live="polite">
      {toasts.map((t) => {
        const left = Math.max(1, Math.ceil((t.shownAt + (t.bad ? BAD_MS : GOOD_MS) - Date.now()) / 1000));
        return (
          <div key={t.id} className="toast" style={t.bad ? { borderColor: "var(--bad)" } : undefined}>
            <span className={t.bad ? "bad" : "warn"}>{t.bad ? "✗" : "●"}</span>
            <span>{t.text}</span>
            {t.bad ? (
              <button className="x" onClick={() => dismiss(t.id)} aria-label="Dismiss">
                {left}s · ✕
              </button>
            ) : (
              <span className="x">fades in {left}s</span>
            )}
          </div>
        );
      })}
    </div>
  );
}
