import { useEffect, useState } from "react";
import { go } from "../store";
import { transport, type view } from "../transport";
import { Keys } from "../components/keys";

// The timeline: a stack's last half hour from its journal, a lane per
// service with the stretches it was awake (the start's own part lighter,
// up to ready) and its crashes, then the events, newest first. Go lays it
// out; this draws fractions. It is read again every five seconds while
// it is on screen, and not at all when it is not.
export function TimelinePage({ stackKey }: { stackKey: string }) {
  const [tl, setTl] = useState<view.Timeline | null>(null);
  useEffect(() => {
    let gone = false;
    const load = () => transport().then(async (t) => {
      const got = await t.timeline(stackKey);
      if (!gone) setTl(got);
    });
    void load();
    const iv = setInterval(() => void load(), 5000);
    return () => {
      gone = true;
      clearInterval(iv);
    };
  }, [stackKey]);
  if (!tl) {
    return (
      <div className="empty">
        <p className="dim">reading the journal…</p>
      </div>
    );
  }
  return (
    <>
      <div className="head">
        <h1>Timeline</h1>
        <span className="path">{tl.stack} · the last half hour, from the journal</span>
        <span className="grow" />
        <button className="btn" onClick={() => void go({ key: stackKey, page: "home" })}>
          Back to {tl.stack} <kbd>esc</kbd>
        </button>
      </div>
      <div className="scrollarea">
        <section className="tl" aria-label="Awake over the last half hour">
          <div className="tl-axis">
            <span />
            <div className="tl-ticks">
              {(tl.ticks ?? []).map((t) => (
                <span key={t.at} style={{ left: `${t.at * 100}%` }}>
                  {t.label}
                </span>
              ))}
            </div>
          </div>
          {(tl.lanes ?? []).map((l) => (
            <div key={l.name} className="tl-lane">
              <button className="tl-name" onClick={() => void go({ key: stackKey, page: "instance", instance: l.name })} title={`Open ${l.name}`}>
                <span className={"dot " + dotClass(l.look.word)} aria-hidden />
                {l.name}
              </button>
              <div className="tl-track">
                {(tl.ticks ?? []).map((t) => (
                  <i key={t.at} className="tl-grid" style={{ left: `${t.at * 100}%` }} />
                ))}
                {(l.spans ?? []).map((s, i) => (
                  <span key={i} className={"tl-span" + (s.open ? " open" : "")} style={{ left: `${s.from * 100}%`, width: `${Math.max(0.4, (s.to - s.from) * 100)}%` }} title={s.title}>
                    {s.ready ? <i className="tl-wake" style={{ width: `${Math.min(100, ((s.ready - s.from) / Math.max(0.0001, s.to - s.from)) * 100)}%` }} /> : null}
                  </span>
                ))}
                {(l.marks ?? []).map((m, i) => (
                  <span key={i} className={"tl-mark " + m.kind} style={{ left: `${m.at * 100}%` }} title={m.title} aria-label={m.title} />
                ))}
              </div>
            </div>
          ))}
          <div className="tl-legend">
            <span>
              <i className="tl-span tl-sw" /> awake
            </span>
            <span>
              <i className="tl-span tl-sw">
                <i className="tl-wake" style={{ width: "100%" }} />
              </i>{" "}
              starting, up to ready
            </span>
            <span>
              <i className="tl-mark crash tl-sw" /> crashed
            </span>
            <span className="grow" />
            <span>hover a stretch for how long, and why it woke</span>
          </div>
        </section>
        <section className="card tl-events">
          <h3>
            What happened <span className="dim">· newest first</span>
          </h3>
          {tl.empty ? (
            <div className="dim">Nothing woke, slept or crashed in the last half hour.</div>
          ) : (
            <div className="mono tl-list">
              {(tl.events ?? []).map((e, i) => (
                <div key={i} style={{ display: "contents" }}>
                  <span className="dim">{e.at}</span>
                  <span className={"tone-" + e.tone}>{e.kind}</span>
                  <span className="ell">{e.text}</span>
                </div>
              ))}
            </div>
          )}
        </section>
      </div>
      <Keys keys={[["esc", "back to the stack"]]} note="from a stack: Timeline in its head, or T" />
    </>
  );
}

function dotClass(word: string): string {
  switch (word) {
    case "awake":
      return "g";
    case "dozing":
      return "y";
    case "crashing":
    case "failed":
      return "r";
  }
  return "";
}
