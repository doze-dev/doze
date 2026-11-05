import { useEffect, useState } from "react";
import { ask, copy, go, run } from "../store";
import type { view } from "../transport";
import { ChartCard } from "../components/chart";
import { KVRows } from "../components/card";
import { Keys } from "../components/keys";
import { Dot } from "../components/sidebar";
import { LogsPage } from "./logs";
import { ConsoleTab, Extends } from "../components/console";

// An instance, as the Instance, Cache, Mailpit, Task and Crash boards lay
// it out: the head line, its verbs, the tabs, and the overview's cards.
export function InstancePage({ inst, stackKey }: { inst: view.Instance; stackKey: string }) {
  const tabs = inst.tabs ?? [];
  const [tab, setTab] = useState(tabs[0]?.id ?? "logs");
  const current = tabs.some((t) => t.id === tab) ? tab : (tabs[0]?.id ?? "logs");
  const task = inst.kind === "task";
  const awake = inst.look.word === "awake" || inst.look.word === "dozing";

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const tag = (e.target as HTMLElement).tagName;
      if (tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA") return;
      switch (e.key) {
        case "Escape":
          void go({ key: stackKey, page: "home" });
          break;
        case "Tab": {
          e.preventDefault();
          const i = tabs.findIndex((t) => t.id === current);
          const next = tabs[(i + (e.shiftKey ? tabs.length - 1 : 1)) % tabs.length];
          if (next) setTab(next.id);
          break;
        }
        case "r":
          task ? void run(stackKey, "task", inst.name) : void ask(stackKey, inst.name, "restart");
          break;
        case "s":
          if (!task && awake) void ask(stackKey, inst.name, "sleep");
          break;
        case "w":
          if (!task && !awake) void run(stackKey, "wake", inst.name);
          break;
        case "p":
          if (inst.kind === "module") void run(stackKey, "pin", inst.name);
          break;
        case "y": {
          const url = inst.connect?.find((r) => r.k === "URL" || r.k === "Address")?.copy;
          if (url) void copy(url, `the connect line of ${inst.name}`);
          break;
        }
        case "l":
          setTab("logs");
          break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [stackKey, inst, tabs, current, task, awake]);

  if (inst.missing) {
    return (
      <div className="empty">
        {inst.stack} has nothing called {inst.name}.
      </div>
    );
  }
  return (
    <>
      <div className={"head" + (inst.banner.ok ? "" : " faded")}>
        {task ? (
          <span className={"tone-" + inst.look.tone} style={{ fontSize: 16 }} aria-hidden>
            {inst.look.glyph}
          </span>
        ) : (
          <Dot look={inst.look} trouble={!!inst.crash} />
        )}
        <h1>{inst.name}</h1>
        <span className="path">{inst.path}</span>
        <span className="grow" />
        {task ? (
          <>
            <span className="dim mono">doze task {inst.name}</span>
            <button className="btn pri" onClick={() => void run(stackKey, "task", inst.name)}>
              Run now <kbd>r</kbd>
            </button>
          </>
        ) : inst.crash ? (
          <>
            <button className="btn" disabled title="Needs work in doze first">
              Stop retrying
            </button>
            <button className="btn pri" onClick={() => void run(stackKey, "restart", inst.name)}>
              Retry now <kbd>r</kbd>
            </button>
          </>
        ) : awake ? (
          <>
            {inst.kind === "module" && (
              <button className="btn" onClick={() => void run(stackKey, "pin", inst.name)}>
                Pin awake <kbd>p</kbd>
              </button>
            )}
            <button className="btn" onClick={() => void ask(stackKey, inst.name, "restart")}>
              Restart <kbd>r</kbd>
            </button>
            <button className="btn" onClick={() => void ask(stackKey, inst.name, "sleep")}>
              Sleep <kbd>s</kbd>
            </button>
          </>
        ) : (
          <button className="btn" onClick={() => void run(stackKey, "wake", inst.name)}>
            Wake <kbd>w</kbd>
          </button>
        )}
      </div>
      <nav className="tabs" role="tablist">
        {tabs.map((t) => (
          <button key={t.id} role="tab" aria-selected={t.id === current} className={"tab" + (t.id === current ? " on" : "")} onClick={() => setTab(t.id)}>
            {t.title}
          </button>
        ))}
        {!task && (
          <button className="tab" disabled title="Needs work in doze first">
            Terminal <span className="new">new</span>
          </button>
        )}
      </nav>
      {current === "overview" && <Overview inst={inst} stackKey={stackKey} />}
      {current === "logs" && <LogsPage stackKey={stackKey} instance={inst.name} embedded inst={inst} />}
      {current === "last run" && <LastRun inst={inst} />}
      {current === "web console" && <WebConsole inst={inst} stackKey={stackKey} />}
      {current.startsWith("console:") && <ConsoleTab stackKey={stackKey} inst={inst.name} tab={current} />}
      {!current.startsWith("console:") && (
      <Keys
        keys={
          task
            ? [
                ["r", "run now"],
                ["l", "logs"],
                ["esc", "back"],
              ]
            : [
                ["esc", "back"],
                ["tab", "next tab"],
                ["y", "copy URL"],
                ...(awake ? ([["s", "sleep"], ["r", "restart"]] as Array<[string, string]>) : ([["w", "wake"]] as Array<[string, string]>)),
              ]
        }
        note={task ? "A task's page is its last run, as in the TUI" : "the same tabs as the TUI's focus page"}
      />
      )}
    </>
  );
}

function Overview({ inst, stackKey }: { inst: view.Instance; stackKey: string }) {
  return (
    <div className="grid2">
      <div className="col">
        {inst.crash && (
          <section className="card danger">
            <h3 className="bad">It exited</h3>
            <div className="mono" style={{ fontSize: 13, lineHeight: "22px" }}>
              <div className="bright">{inst.crash.error || "no error text"}</div>
              {inst.crash.where && <div className="dim">{inst.crash.where}</div>}
            </div>
            <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
              <button className="btn" onClick={() => void copy([inst.crash?.error, inst.crash?.where].filter(Boolean).join("\n"), "the error")}>
                Copy the error
              </button>
            </div>
          </section>
        )}
        {inst.crash && (inst.crash.tries || inst.crash.nextIn) && (
          <section className="card">
            <h3>What doze does next</h3>
            <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
              <div className="bar" style={{ flex: 1, height: 8 }}>
                <i className="c4" style={{ width: `${inst.crash.fraction * 100}%` }} />
              </div>
              <span className="bright">{inst.crash.tries}</span>
            </div>
            <div>
              {inst.crash.gaveUp ? (
                <>It stopped retrying and waits for you; nothing else in the stack is touched.</>
              ) : (
                <>
                  next try in <b className="bright">{inst.crash.nextIn || "a moment"}</b>. After the {inst.crash.lastTry || "last"} it stops and waits for you; nothing else in the stack is touched.
                </>
              )}
            </div>
          </section>
        )}
        {inst.crash && (inst.crash.lastRun ?? []).length > 0 && (
          <section className="card" style={{ flex: 1 }}>
            <h3>Its last run</h3>
            <div className="mono" style={{ display: "flex", flexDirection: "column", gap: 1 }}>
              {(inst.crash.lastRun ?? []).map((l, i) => (
                <div key={i}>
                  <span className="dim">{l.time}</span> <span className={l.rank >= 3 ? "bad" : l.rank === 2 ? "warn" : ""}>{l.line}</span>
                </div>
              ))}
            </div>
          </section>
        )}
        {(inst.connect ?? []).length > 0 && (
          <section className="card">
            <h3>Connect</h3>
            <KVRows rows={inst.connect ?? []} />
          </section>
        )}
        <Extends after="connect" inst={inst} stackKey={stackKey} />
        <section className="card">
          <h3>{inst.look.word === "awake" ? "Why it's awake" : "Awake because"}</h3>
          {(inst.holders ?? []).map((p) => (
            <div key={p.text} className="why">
              <span className="pill">
                <span className="dot g" /> {p.text}
              </span>
              <span className="dim">{p.explain}</span>
            </div>
          ))}
          {inst.dozing && (
            <div className="why">
              <span className="pill">
                <span className="dot y" /> <span className="warn">sleeps in {inst.dozing.left}</span>
              </span>
              <div className="bar" style={{ flex: 1 }}>
                <i className="c3" style={{ width: `${inst.dozing.fraction * 100}%` }} />
              </div>
            </div>
          )}
          {(inst.holders ?? []).length === 0 && !inst.dozing && inst.sleeps && <div className="bright">{inst.sleeps}</div>}
          {((inst.holders ?? []).length > 0 || inst.dozing) && <div className="dim">{inst.sleeps}</div>}
        </section>
        <Extends after="awake" inst={inst} stackKey={stackKey} />
        {(inst.needs ?? []).length > 0 && (
          <section className="card">
            <h3>Needs</h3>
            <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
              {(inst.needs ?? []).map((n) => (
                <button key={n.name} className="pill" onClick={() => n.known && void go({ key: stackKey, page: "instance", instance: n.name })}>
                  <span className={"tone-" + n.look.tone}>{n.glyph}</span> {n.name} <span className="dim">{n.look.word}</span>
                </button>
              ))}
            </div>
          </section>
        )}
        <section className="card" style={{ flex: 1 }}>
          <h3>Recent</h3>
          <div className="mono recent">
            {(inst.recent ?? []).map((r, i) => (
              <div key={i}>
                <span className="dim">{r.at}</span> <span className={"tone-" + r.tone}>{r.kind}</span> {r.text}
              </div>
            ))}
            {(inst.recent ?? []).length === 0 && <div className="dim">nothing yet</div>}
          </div>
        </section>
        <Extends after="recent" inst={inst} stackKey={stackKey} />
      </div>
      <div className="col">
        {inst.vitals ? (
          <>
            <ChartCard title="Memory" chart={inst.vitals.ram} color="cool" tall />
            <ChartCard title="CPU" chart={inst.vitals.cpu} color="accent" tall />
            <ChartCard title="Connections" chart={inst.vitals.conns} color="good" />
            <Extends after="vitals" inst={inst} stackKey={stackKey} />
          </>
        ) : (
          <section className="card">
            <h3>Vitals</h3>
            <div className="dim">{inst.look.word === "asleep" ? "Asleep, it costs nothing until something connects; its data stays." : "no readings yet"}</div>
          </section>
        )}
      </div>
    </div>
  );
}

function LastRun({ inst }: { inst: view.Instance }) {
  const runs = inst.runs ?? [];
  const last = runs[0];
  return (
    <div className="grid2" style={{ gridTemplateColumns: "minmax(0,1fr) 340px" }}>
      <div className="col">
        <div className="dock" style={{ margin: 0 }}>
          <div className="cap">
            <b>{inst.name}</b>
            <span>{last ? `ran ${last.when} · ${last.ok ? "ok" : "failed"} · ${last.took}` : "has not run"}</span>
            <span className="grow" />
            <span>its output</span>
          </div>
          <LogsPage stackKey={inst.key} instance={inst.name} embedded bare />
        </div>
      </div>
      <div className="col">
        <section className="box" style={{ flex: 1 }}>
          <h3>history</h3>
          <div className="hist h">
            <span />
            <span>when</span>
            <span>why</span>
            <span className="r">took</span>
          </div>
          {runs.map((r) => (
            <div key={r.at} className="hist">
              <span className={r.done ? (r.ok ? "ok" : "bad") : "cool"}>{r.done ? (r.ok ? "✓" : "✗") : "◌"}</span>
              <span className="mono">{r.when}</span>
              <span>{r.why}</span>
              <span className="r">{r.took}</span>
            </div>
          ))}
          {runs.length === 0 && <div className="dim">it has not run yet · r run now</div>}
        </section>
      </div>
    </div>
  );
}

function WebConsole({ inst, stackKey }: { inst: view.Instance; stackKey: string }) {
  const asleep = inst.look.word === "asleep";
  return (
    <div className="grid2" style={{ gridTemplateColumns: "minmax(0,1.3fr) minmax(0,1fr)" }}>
      <div className="col">
        <section className="card">
          <h3>{inst.name} has its own console</h3>
          <div className="mono bright" style={{ fontSize: 13 }}>
            {inst.web}
          </div>
          {asleep && <div>It is asleep. Opening the console wakes it first.</div>}
          <div style={{ display: "flex", gap: 8, marginTop: 4 }}>
            <a className="btn pri" href={inst.web} target="_blank" rel="noreferrer">
              Open in browser <kbd>o</kbd>
            </a>
            {asleep && (
              <button className="btn" onClick={() => void run(stackKey, "wake", inst.name)}>
                Just wake it <kbd>w</kbd>
              </button>
            )}
          </div>
        </section>
      </div>
      <div className="col">
        {(inst.connect ?? []).length > 0 && (
          <section className="card">
            <h3>what doze can still tell you</h3>
            <KVRows rows={inst.connect ?? []} />
          </section>
        )}
      </div>
    </div>
  );
}
