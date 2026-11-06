import { go, set, useStore } from "../store";

// While the window was closed (the WhileAway board): what happened in the
// stacks meanwhile, said once, on whichever page the window opened to.
// Got it, or opening Needs you, puts it away; what is still wrong stays in
// the bell.
export function AwayStrip() {
  const away = useStore((s) => s.away);
  if (!away || (away.events ?? []).length === 0) return null;
  return (
    <section className="away" aria-label="While the window was closed">
      <div className="ah">
        <span className="dot c" aria-hidden />
        <b>While the window was closed</b>
        <span className="dim">{away.for} · the stacks ran on in the menu bar</span>
        <span className="grow" />
        <button className="btn sm" onClick={() => set({ away: null, bell: true, alerts: [] })}>
          Open Needs you
        </button>
        <button className="btn quiet sm" onClick={() => set({ away: null })}>
          Got it
        </button>
      </div>
      {(away.events ?? []).map((e, i) => (
        <button key={i} className="ev" onClick={() => void go({ key: e.key, page: "instance", instance: e.instance })} title={`Open ${e.instance}`}>
          <span className={"dot " + (e.tone === "bad" ? "r" : e.tone === "good" ? "g" : "")} aria-hidden />
          <span className="tm">{e.at}</span>
          <span className="w">
            <b>{e.instance}</b> {e.what}
          </span>
          <span className={"st " + e.tone}>{e.now}</span>
        </button>
      ))}
    </section>
  );
}
