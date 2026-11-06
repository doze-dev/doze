import { useEffect, useState } from "react";
import { go, set, useStore } from "../store";
import { Alerts } from "./bell";

const GOOD_MS = 4000;
const BAD_MS = 12000;

// Toasts at the bottom right: a success fades after 4 s, a failure stays
// 12 s or until dismissed, as in the TUI.
export function Toasts() {
  const toasts = useStore((s) => s.toasts);
  const where = useStore((s) => s.where);
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
  const alerts = useStore((s) => s.alerts.length);
  if (toasts.length === 0 && alerts === 0) return null;
  const dismiss = (id: number) => set((s) => ({ toasts: s.toasts.filter((x) => x.id !== id) }));
  return (
    <div className="toasts" aria-live="polite">
      <Alerts />
      {toasts.map((t) => (
        <div key={t.id} className="toast" style={t.bad ? { borderColor: "var(--bad)" } : undefined}>
          <span className={"dot " + (t.bad ? "r" : "g")} aria-hidden style={{ width: 10, height: 10 }} />
          <div className="t">
            <b>{t.text}</b>
          </div>
          {t.bad && where?.key && (
            <button className="btn quiet" style={{ height: 24 }} onClick={() => void go({ key: where.key, page: "recent" })} title="Everything said lately, in full">
              All
            </button>
          )}
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
