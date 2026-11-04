import { useCallback, useEffect, useRef, useState } from "react";
import { useStore } from "../store";
import { transport, type view } from "../transport";
import { LogList } from "../components/vlist";
import { Keys } from "../components/keys";
import { Sparkline } from "../components/chart";
import { Dot } from "../components/sidebar";

const WINDOW = 200;

// Logs, as the Logs and AppLogs boards lay them out: the level and time
// segments, the search, the run stepping, then the lines in a dock. Merged
// across the stack ("" instance) the Show list in the sidebar picks the
// instances and doze's own events ride along. Filtering is Go's; the page
// asks for a window and is told when there is more.
export function LogsPage({
  stackKey,
  instance,
  embedded,
  bare,
  inst,
}: {
  stackKey: string;
  instance: string;
  embedded?: boolean;
  /** bare is the lines only, inside another dock (a task's output). */
  bare?: boolean;
  inst?: view.Instance;
}) {
  const logsVersion = useStore((s) => s.logsVersion);
  const show = useStore((s) => s.show);
  const [level, setLevel] = useState(1);
  const [search, setSearch] = useState("");
  const [runIdx, setRunIdx] = useState(0); // 0 = every run; otherwise an index into logs.runs from the newest
  const [clock, setClock] = useState(true);
  const [logs, setLogs] = useState<view.Logs | null>(null);
  const [pinned, setPinned] = useState(true);
  const want = useRef(0);
  const searchBox = useRef<HTMLInputElement>(null);

  const query = useCallback(
    (from: number, n: number): view.LogsQuery => {
      const runs = logs?.runs ?? [];
      const run = runIdx > 0 ? (runs[runs.length - runIdx] ?? 0) : 0;
      return { instance, show: instance ? undefined : (show ?? undefined), level, search, run, from, n };
    },
    [instance, show, level, search, runIdx, logs?.runs],
  );

  // The newest window on every change; the pin keeps the end in view.
  useEffect(() => {
    void (async () => {
      const t = await transport();
      const probe = await t.logs(stackKey, query(0, 1));
      const from = pinned ? Math.max(0, probe.total - WINDOW) : want.current;
      setLogs(await t.logs(stackKey, query(from, WINDOW)));
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stackKey, instance, show, level, search, runIdx, logsVersion, pinned]);

  const onNeed = useCallback(
    (from: number) => {
      want.current = from;
      transport().then(async (t) => setLogs(await t.logs(stackKey, query(from, WINDOW))));
    },
    [stackKey, query],
  );

  useEffect(() => {
    if (bare) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const tag = (e.target as HTMLElement).tagName;
      if (tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA") {
        if (e.key === "Escape") (e.target as HTMLElement).blur();
        return;
      }
      switch (e.key) {
        case "/":
          e.preventDefault();
          searchBox.current?.focus();
          break;
        case "f":
          setLevel((l) => (l + 1) % 4);
          break;
        case "t":
          setClock((c) => !c);
          break;
        case "[":
          setRunIdx((i) => Math.min(i + 1, logs?.runs?.length ?? 0));
          break;
        case "]":
          setRunIdx((i) => Math.max(i - 1, 0));
          break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [bare, logs?.runs?.length]);

  const runs = logs?.runs ?? [];
  const list = logs && (
    <LogList
      lines={logs.lines ?? []}
      total={logs.total}
      from={logs.from}
      merged={instance === ""}
      instances={logs.instances ?? []}
      clock={clock}
      search={search}
      onNeed={onNeed}
      pinned={pinned}
      onPinned={setPinned}
    />
  );
  if (bare) return <div className="lg mono">{list}</div>;

  return (
    <>
      {!embedded && (
        <div className="head" style={{ gap: 10 }}>
          <label className="search big grow">
            <span className="dim">/</span>
            <input ref={searchBox} aria-label="Search logs" placeholder="search" value={search} onChange={(e) => setSearch(e.target.value)} />
            {search && (
              <span className="dim">
                {logs?.matches ?? 0} {logs?.matches === 1 ? "match" : "matches"}
              </span>
            )}
          </label>
          <select className="pill" aria-label="Level" value={level} onChange={(e) => setLevel(Number(e.target.value))}>
            {(logs?.levels ?? LEVELS).map((name, i) => (
              <option key={name} value={i}>
                Level: {name}
              </option>
            ))}
          </select>
          <select className="pill" aria-label="Run" value={runIdx} onChange={(e) => setRunIdx(Number(e.target.value))}>
            <option value={0}>Run: every run</option>
            {runs.map((r, i) => (
              <option key={r} value={runs.length - i}>
                Run: {i === runs.length - 1 ? "this one" : `${r}`}
              </option>
            ))}
          </select>
          <button className="pill" onClick={() => setClock((c) => !c)} aria-label="Time">
            Time: {clock ? "clock" : "relative"} ▾
          </button>
          <button className={"btn" + (pinned ? " pri" : "")} onClick={() => setPinned(true)}>
            {pinned ? "Following" : "↓ Follow"}
          </button>
        </div>
      )}
      {embedded && (
        <div className="lbar">
          <div className="seg" aria-label="Level">
            {LEVELS.map((name, i) => (
              <button key={name} className={i === level ? "on" : ""} onClick={() => setLevel(i)}>
                {name}
              </button>
            ))}
          </div>
          <label className="search">
            <span className="dim">/</span>
            <input ref={searchBox} aria-label="Search" placeholder="search" value={search} onChange={(e) => setSearch(e.target.value)} />
            {search && <span className="dim">{logs?.matches ?? 0}</span>}
          </label>
          <span className="grow" />
          <div className="seg" aria-label="Time">
            <button className={clock ? "on" : ""} onClick={() => setClock(true)}>
              clock
            </button>
            <button className={!clock ? "on" : ""} onClick={() => setClock(false)}>
              relative
            </button>
          </div>
          {runs.length > 1 && (
            <>
              <button className="btn small" onClick={() => setRunIdx((i) => Math.min(i + 1, runs.length))} disabled={runIdx >= runs.length} aria-label="Earlier run">
                ‹ <kbd>[</kbd>
              </button>
              <span className="dim small">{runIdx === 0 ? `every run` : `run ${runs.length - runIdx + 1} of ${runs.length}`}</span>
              <button className="btn small" onClick={() => setRunIdx((i) => Math.max(i - 1, 0))} disabled={runIdx === 0} aria-label="Later run">
                <kbd>]</kbd> ›
              </button>
            </>
          )}
        </div>
      )}
      <div className="logs-body">
        <div className="dock" style={{ margin: 0 }}>
          <div className="cap">
            <b>{instance || "all logs"}</b>
            <span>
              {pinned ? "following" : "paused"} · {LEVELS[level]}
            </span>
            <span className="grow" />
            <span>
              {logs?.total ?? 0} lines · {runIdx === 0 ? "every run" : "this run"} · {clock ? "local time" : "relative"}
            </span>
          </div>
          {list}
        </div>
        {embedded && inst && <About inst={inst} logs={logs} />}
      </div>
      {!embedded && (
        <Keys
          keys={[
            ["/", "search"],
            ["f", "level"],
            ["[ ]", "run"],
            ["t", "time"],
          ]}
          note="select text with the mouse, as in any window"
        />
      )}
    </>
  );
}

const LEVELS = ["debug and up", "info and up", "warnings and up", "errors only"];

// About is the AppLogs board's right column: needs, this run, processes,
// levels.
function About({ inst, logs }: { inst: view.Instance; logs: view.Logs | null }) {
  const url = inst.connect?.find((r) => r.k === "URL" || r.k === "Address")?.v;
  return (
    <aside className="rcol" aria-label="About this run">
      {(inst.needs ?? []).length > 0 && (
        <section className="box">
          <h3>needs</h3>
          {(inst.needs ?? []).map((n) => (
            <div key={n.name} className="nd">
              <Dot look={n.look} />
              {n.name}
              <small>{n.task ? (n.look.word === "done" ? "ran first" : n.look.word) : n.look.word}</small>
            </div>
          ))}
        </section>
      )}
      <section className="box">
        <h3>
          this run <span style={{ textTransform: "none", letterSpacing: 0, fontWeight: 400 }}>· last {inst.vitals?.ram.span ?? "run"}</span>
        </h3>
        {url && (
          <div className="nd">
            <span className="k">url</span>
            <span className="mono bright">{url}</span>
          </div>
        )}
        {inst.up && (
          <div className="nd">
            <span className="k">up</span>
            <span>
              {inst.up}
              {inst.pid ? ` · pid ${inst.pid}` : ""}
            </span>
          </div>
        )}
        {inst.vitals && (
          <>
            <div className="nd">
              <span className="k">cpu</span>
              <Sparkline points={inst.vitals.cpu.points ?? []} color="accent" />
              <small>{inst.vitals.cpu.peak}</small>
            </div>
            <div className="nd">
              <span className="k">mem</span>
              <Sparkline points={inst.vitals.ram.points ?? []} color="cool" />
              <small>{inst.vitals.ram.now}</small>
            </div>
          </>
        )}
      </section>
      {(inst.procs ?? []).length > 0 && (
        <section className="box">
          <h3>
            Processes <span className="new">new</span>
          </h3>
          <div className="pt">
            <span className="dim">command</span>
            <span className="dim r">pid</span>
            <span className="dim r">mem</span>
          </div>
          {(inst.procs ?? []).map((p) => (
            <div key={p.pid} className="pt">
              <span className={p.depth === 0 ? "bright" : ""}>{(p.depth > 0 ? "└ " : "") + p.cmd}</span>
              <span className="r">{p.pid}</span>
              <span className="r">{p.mem}</span>
            </div>
          ))}
        </section>
      )}
      {logs && (
        <section className="box">
          <h3>levels</h3>
          <div>
            {logs.counts[3] > 0 && <span className="bad">{logs.counts[3]} errors · </span>}
            {logs.counts[2] > 0 && <span className="warn">{logs.counts[2]} warn · </span>}
            {logs.counts[1]} info
          </div>
          <div className="dim">{logs.counts[0] > 0 ? `${logs.counts[0]} debug lines` : "no debug lines"}</div>
        </section>
      )}
    </aside>
  );
}
