import { useState } from "react";
import { go, run } from "../store";
import type { view } from "../transport";
import { Chart } from "../components/chart";
import { LogsPage } from "./logs";

// An instance: its state and story at the top, then its tabs, as the dash
// laid them out: overview (vitals, needs, address), logs, a task's runs.
export function InstancePage({ inst, stackKey }: { inst: view.Instance; stackKey: string }) {
  const tabs = inst.tabs ?? [];
  const [tab, setTab] = useState(tabs[0]?.id ?? "logs");
  const current = tabs.some((t) => t.id === tab) ? tab : (tabs[0]?.id ?? "logs");
  if (inst.missing) {
    return (
      <div className="instance">
        <p className="muted">
          {inst.stack} has nothing called {inst.name}.
        </p>
      </div>
    );
  }
  const task = inst.kind === "task";
  return (
    <div className={"instance tone-" + inst.look.tone + (inst.banner.ok ? "" : " faded")}>
      <header className="page-head">
        <button className="back" onClick={() => void go({ key: stackKey, page: "home" })} aria-label="Back to the stack">
          ‹ {inst.stack}
        </button>
        <h1>
          <span className="glyph" aria-hidden>
            {inst.look.glyph}
          </span>{" "}
          {inst.name}
        </h1>
        <span className="word">{inst.look.word}</span>
        {inst.up && <span className="muted">up {inst.up}</span>}
        <span className="verbs">
          {task ? (
            <button onClick={() => void run(stackKey, "task", inst.name)}>run now</button>
          ) : (
            <>
              <button onClick={() => void run(stackKey, "wake", inst.name)} disabled={inst.look.word === "awake" || inst.look.word === "dozing"}>
                wake
              </button>
              <button onClick={() => void run(stackKey, "sleep", inst.name)} disabled={!(inst.look.word === "awake" || inst.look.word === "dozing")}>
                sleep
              </button>
              <button onClick={() => void run(stackKey, "restart", inst.name)}>restart</button>
            </>
          )}
        </span>
      </header>
      <p className="story">
        <span>{inst.what}</span>
        <span className="muted"> · {inst.because}</span>
        {inst.hint && <span className="muted"> · {inst.hint}</span>}
      </p>
      {inst.lastError && <p className="notice bad">{inst.lastError}</p>}
      <nav className="subtabs" role="tablist">
        {tabs.map((t) => (
          <button key={t.id} role="tab" aria-selected={t.id === current} className={t.id === current ? "current" : ""} onClick={() => setTab(t.id)}>
            {t.title}
          </button>
        ))}
      </nav>
      {current === "overview" && <Overview inst={inst} />}
      {current === "logs" && <LogsPage stackKey={stackKey} instance={inst.name} embedded />}
      {current === "last run" && <Runs runs={inst.runs ?? []} />}
      {current === "web console" && (
        <div className="web">
          <a href={inst.web} target="_blank" rel="noreferrer">
            {inst.web}
          </a>
        </div>
      )}
    </div>
  );
}

function Overview({ inst }: { inst: view.Instance }) {
  return (
    <div className="overview">
      {inst.address && (
        <dl className="facts">
          <dt>address</dt>
          <dd>
            <code>{inst.address}</code>
          </dd>
          {inst.url && (
            <>
              <dt>connect</dt>
              <dd>
                <code>{inst.url}</code>
              </dd>
            </>
          )}
          {inst.inUse && (
            <>
              <dt>in use by</dt>
              <dd>{inst.inUse}</dd>
            </>
          )}
          {inst.pid ? (
            <>
              <dt>pid</dt>
              <dd>{inst.pid}</dd>
            </>
          ) : null}
        </dl>
      )}
      {inst.vitals && (
        <div className="vitals">
          <Chart label="memory" chart={inst.vitals.ram} />
          <Chart label="cpu" chart={inst.vitals.cpu} />
          <Chart label="connections" chart={inst.vitals.conns} />
        </div>
      )}
      {(inst.needs ?? []).length > 0 && (
        <div className="needs">
          <span className="muted">needs</span>
          {(inst.needs ?? []).map((n) => (
            <span key={n.name} className={"need tone-" + n.look.tone}>
              <span className="glyph" aria-hidden>
                {n.glyph}
              </span>{" "}
              {n.name}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

function Runs({ runs }: { runs: view.Run[] }) {
  if (runs.length === 0) return <p className="muted">It has not run yet.</p>;
  return (
    <ol className="run-list">
      {runs.map((r) => (
        <li key={r.at} className={r.done ? (r.ok ? "tone-good" : "tone-bad") : "tone-cool"}>
          <span className="glyph" aria-hidden>
            {r.done ? (r.ok ? "✓" : "✗") : "◌"}
          </span>
          <span className="ago">{r.ago} ago</span>
          <span className="why">{r.why}</span>
          <span className="muted">{r.took}</span>
        </li>
      ))}
    </ol>
  );
}
