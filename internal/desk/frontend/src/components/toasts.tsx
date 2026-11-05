import { useEffect, useState } from "react";
import { set, useStore } from "../store";

const GOOD_MS = 4000;
const BAD_MS = 12000;

// Toasts at the bottom right: a success fades after 4 s, a failure stays
// 12 s or until dismissed, as in the TUI.
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
      {toasts.map((t) => (
        <div key={t.id} className="toast" style={t.bad ? { borderColor: "var(--bad)" } : undefined}>
          <span className={"dot " + (t.bad ? "r" : "g")} aria-hidden style={{ width: 10, height: 10 }} />
          <div className="t">
            <b>{t.text}</b>
          </div>
          {t.bad && (
            <button className="ib" onClick={() => dismiss(t.id)} aria-label="Dismiss">
              ×
            </button>
          )}
        </div>
      ))}
    </div>
  );
}
