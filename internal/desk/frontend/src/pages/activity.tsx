import { useEffect, useState } from "react";
import { go } from "../store";
import { transport, type view } from "../transport";
import { Keys } from "../components/keys";

// Activity (2026-11-06, replacing the half-hour timeline): what a stack did
// over the last hour, day or week, from its journal on disk. The top line
// answers whether sleeping saves anything (asleep while up, memory left
// free); each service says how long it slept, how often it woke and for
// what, and draws its stretches; trouble comes before the full list. Go
// works out every figure; this draws them. Read again every ten seconds
// while on screen.
const SPANS: [string, string][] = [
  ["1h", "Last hour"],
  ["24h", "24 hours"],
  ["7d", "7 days"],
];

export function ActivityPage({ stackKey, inst }: { stackKey: string; inst?: string }) {
  const [span, setSpan] = useState("24h");
  const [only, setOnly] = useState(inst ?? "");
  const [a, setA] = useState<view.Activity | null>(null);
  useEffect(() => {
    let gone = false;
    const load = () =>
      transport().then(async (t) => {
        const got = await t.activity(stackKey, span, only);
        if (!gone) setA(got);
      });
    void load();
    const iv = setInterval(() => void load(), 10000);
    return () => {
      gone = true;
      clearInterval(iv);
    };
  }, [stackKey, span, only]);
  if (!a) {
    return (
      <div className="empty">
        <p className="dim">reading the journal…</p>
      </div>
    );
  }
  const s = a.summary;
  const label = SPANS.find(([k]) => k === a.span)?.[1].toLowerCase() ?? a.span;
  return (
    <>
      <div className="head">
        <h1>Activity</h1>
        <span className="path">
          {a.stack}
          {only ? ` · ${only}` : ""} · {label === "last hour" ? "the last hour" : `the last ${label}`}
        </span>
        <span className="grow" />
        {only && (
          <button className="btn quiet" onClick={() => setOnly("")}>
            Every service
          </button>
        )}
        <div className="seg" role="group" aria-label="Span">
          {SPANS.map(([k, l]) => (
            <button key={k} className={a.span === k ? "on" : ""} aria-pressed={a.span === k} onClick={() => setSpan(k)}>
              {l}
            </button>
          ))}
        </div>
        <button className="btn" onClick={() => void go({ key: stackKey, page: "home" })}>
          Back to {a.stack} <kbd>esc</kbd>
        </button>
      </div>
      <div className="scrollarea">
        <section className="act-sum" aria-label="Summary">
          <div className="stat">
            <span className="v">{s.asleep}</span>
            <span className="s">asleep while the stack was up</span>
          </div>
          <div className="stat">
            <span className="v">{s.freed ? s.freed.replace(/^about /, "~") : "—"}</span>
            <span className="s" title={s.freedNote}>
              memory left free by sleeping{s.freedNote ? " ⓘ" : ""}
            </span>
          </div>
          <div className="stat">
            <span className="v">{s.wakes}</span>
            <span className="s">wakes</span>
          </div>
          <div className="stat">
            <span className={"v" + (s.crashes ? " bad" : "")}>{s.crashes}</span>
            <span className="s">crashes and failed starts</span>
          </div>
          <div className="stat">
            <span className="v">{s.up.split(" of ")[0]}</span>
            <span className="s">up, of {s.up.split(" of ")[1]}</span>
          </div>
        </section>
        {a.short && <p className="model act-short">{a.short}</p>}
        {(a.trouble ?? []).length > 0 && (
          <section className="card act-trouble" aria-label="Trouble">
            <h3>Trouble</h3>
            <div className="mono tl-list">
              {(a.trouble ?? []).map((e, i) => (
                <div key={i} style={{ display: "contents" }}>
                  <span className="dim">{e.at}</span>
                  <span className="tone-bad">{e.kind}</span>
                  <span className="ell">{e.text}</span>
                </div>
              ))}
            </div>
          </section>
        )}
        <section className="tl act" aria-label="Each service">
          <div className="act-row h">
            <span>Service</span>
            <div className="tl-ticks">
              {(a.ticks ?? []).map((t) => (
                <span key={t.at} style={{ left: `${t.at * 100}%` }}>
                  {t.label}
                </span>
              ))}
            </div>
            <span>Asleep</span>
            <span className="r">Wakes</span>
            <span>Woken by</span>
            <span className="r">Freed</span>
          </div>
          {(a.rows ?? []).map((r) => (
            <div key={r.name} className="act-row">
              <button className="tl-name" onClick={() => setOnly(only === r.name ? "" : r.name)} title={only === r.name ? "Every service" : `Only ${r.name}`}>
                <span className={"dot " + dotClass(r.look.word)} aria-hidden />
                {r.name}
              </button>
              <div className="tl-track">
                {(a.ticks ?? []).map((t) => (
                  <i key={t.at} className="tl-grid" style={{ left: `${t.at * 100}%` }} />
                ))}
                {(r.spans ?? []).map((sp, i) => (
                  <span key={i} className={"tl-span" + (sp.open ? " open" : "")} style={{ left: `${sp.from * 100}%`, width: `${Math.max(0.4, (sp.to - sp.from) * 100)}%` }} title={sp.title} tabIndex={0} role="img" aria-label={`${r.name}: ${sp.title}`}>
                    {sp.ready ? <i className="tl-wake" style={{ width: `${Math.min(100, ((sp.ready - sp.from) / Math.max(0.0001, sp.to - sp.from)) * 100)}%` }} /> : null}
                  </span>
                ))}
                {(r.marks ?? []).map((m, i) => (
                  <span key={i} className={"tl-mark " + m.kind} style={{ left: `${m.at * 100}%` }} title={m.title} aria-label={m.title} />
                ))}
              </div>
              <span className="act-asleep" title={r.asleep}>
                <span className="act-bar" aria-hidden>
                  <i style={{ width: `${r.asleepPart * 100}%` }} />
                </span>
                <span className={r.never ? "warn" : ""}>{r.never ? "never" : `${Math.round(r.asleepPart * 100)}%`}</span>
              </span>
              <span className="r">{r.wakes || <span className="dim">0</span>}</span>
              <span className="dim ell" title={r.wokenBy}>
                {r.wokenBy || "—"}
                {r.crashes ? <span className="bad"> · {r.crashes} crashed</span> : null}
              </span>
              <span className="r">{r.freed || <span className="dim">—</span>}</span>
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
            <span>a name shows that service alone; time with the stack down is not counted as sleep</span>
          </div>
        </section>
        <section className="card tl-events">
          <h3>
            What happened <span className="dim">· newest first</span>
          </h3>
          {a.empty ? (
            <div className="dim">Nothing woke, slept or crashed in this span.</div>
          ) : (
            <div className="mono tl-list">
              {(a.events ?? []).map((e, i) => (
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
      <Keys keys={[["esc", "back to the stack"]]} note="from a stack: Activity in its head, or A; from ⌘K: activity" />
    </>
  );
}

function dotClass(word: string): string {
  switch (word) {
    case "awake":
      return "g";
    case "dozing":
      return "y";
    case "waking":
      return "c";
    case "crashing":
    case "failed":
      return "r";
  }
  return "";
}
