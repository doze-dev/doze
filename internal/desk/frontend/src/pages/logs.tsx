import { useCallback, useEffect, useRef, useState } from "react";
import { copy, useStore } from "../store";
import { transport, type view } from "../transport";
import { pageKey } from "../keys";
import { LogList } from "../components/vlist";
import { Keys } from "../components/keys";

const WINDOW = 200;

// Logs, as the InstanceLogs and AllLogs boards lay them out: a filter, the
// level, the run and the time as segments, then the lines in a dock that
// follows. Merged across the stack ("" instance) the Show list in the
// sidebar picks the instances and doze's own events ride along. Filtering
// is Go's; the page asks for a window and is told when there is more.
export function LogsPage({
  stackKey,
  instance,
  embedded,
  bare,
  run: fixedRun,
}: {
  stackKey: string;
  instance: string;
  embedded?: boolean;
  /** bare is the lines only, inside another dock (a task's output). */
  bare?: boolean;
  /** run pins the lines to one run (a task's chosen run); unset follows the run filter. */
  run?: number;
  inst?: view.Instance;
}) {
  const logsVersion = useStore((s) => s.logsVersion);
  const show = useStore((s) => s.show);
  const [level, setLevel] = useState(1);
  const [search, setSearch] = useState("");
  const [runIdx, setRunIdx] = useState(0); // 0 = every run; otherwise an index into logs.runs from the newest
  const [clock, setClock] = useState(false);
  const [logs, setLogs] = useState<view.Logs | null>(null);
  const [pinned, setPinned] = useState(true);
  const want = useRef(0);
  const searchBox = useRef<HTMLInputElement>(null);

  // copyShown copies the lines on screen as they read: time, who (on the
  // merged page), level, line.
  const copyShown = async () => {
    const lines = logs?.lines ?? [];
    const text = lines.map((l) => [l.time, l.instance, l.level, l.line].filter(Boolean).join("  ")).join("\n");
    await copy(text, lines.length === 1 ? "1 line" : `${lines.length} lines`);
  };

  const query = useCallback(
    (from: number, n: number): view.LogsQuery => {
      const runs = logs?.runs ?? [];
      const run = fixedRun ?? (runIdx > 0 ? (runs[runs.length - runIdx] ?? 0) : 0);
      return { instance, show: instance ? undefined : (show ?? undefined), level, search, run, from, n };
    },
    [instance, show, level, search, runIdx, logs?.runs, fixedRun],
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
      const tag = (e.target as HTMLElement).tagName;
      if ((tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA") && e.key === "Escape") {
        (e.target as HTMLElement).blur();
        e.preventDefault();
        return;
      }
      if (!pageKey(e)) {
        return;
      }
      switch (e.key) {
        case "/":
          e.preventDefault();
          searchBox.current?.focus();
          break;
        case "1":
        case "2":
        case "3":
        case "4":
          setLevel(Number(e.key) - 1);
          break;
        case "c":
          void copyShown();
          break;
        case "t":
          setClock((c) => !c);
          break;
        case " ":
          e.preventDefault();
          setPinned((p) => !p);
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

  const total = logs?.total ?? 0;
  return (
    <>
      {!embedded && (
        <div className="head">
          <h1>All logs</h1>
          <span className="path">every service, merged · {pinned ? "following" : "paused"}</span>
          <span className="grow" />
          <button className="btn" onClick={() => setPinned((p) => !p)}>
            {pinned ? "Pause" : "Follow"} <kbd>space</kbd>
          </button>
        </div>
      )}
      <div className="toolbar" style={{ paddingTop: embedded ? 12 : 0 }}>
        <label className="filter">
          <input ref={searchBox} aria-label="Filter the lines" placeholder="Filter text" value={search} onChange={(e) => setSearch(e.target.value)} />
          {search && (
            <span className="dim" style={{ whiteSpace: "nowrap" }}>
              {logs?.matches ?? 0} of {total}
            </span>
          )}
          <kbd>/</kbd>
        </label>
        <div className="seg" role="group" aria-label="Level">
          {LEVELS.map((name, i) => (
            <button key={name} className={i === level ? "on" : ""} aria-pressed={i === level} onClick={() => setLevel(i)}>
              {name}
            </button>
          ))}
        </div>
        {runs.length > 1 && (
          <div className="seg" role="group" aria-label="Run">
            <button className={runIdx === 1 ? "on" : ""} onClick={() => setRunIdx(1)}>
              This run
            </button>
            <button className={runIdx === 0 ? "on" : ""} onClick={() => setRunIdx(0)}>
              All {runs.length} runs
            </button>
            {runIdx > 1 && <button className="on">run {runs.length - runIdx + 1} of {runs.length}</button>}
          </div>
        )}
        <div className="seg" role="group" aria-label="Time">
          <button className={!clock ? "on" : ""} onClick={() => setClock(false)}>
            Relative
          </button>
          <button className={clock ? "on" : ""} onClick={() => setClock(true)}>
            Clock
          </button>
        </div>
        <span className="grow" />
        {logs && (
          <span className="dim" style={{ fontSize: 12 }}>
            {logs.counts[3] > 0 && <span className="bad">{logs.counts[3]} errors · </span>}
            {logs.counts[2] > 0 && <span className="warn">{logs.counts[2]} warnings · </span>}
            {total} lines kept
          </span>
        )}
        {logs && (logs.lines ?? []).length > 0 && (
          <button className="btn" onClick={() => void copyShown()} title="The lines shown, with their times">
            Copy <kbd>c</kbd>
          </button>
        )}
      </div>
      <div className="dock" style={{ marginTop: 0 }}>
        {list}
        <div className="cap" style={{ borderTop: "1px solid var(--line)", borderBottom: 0 }}>
          <span>{pinned ? "▼ following · new lines appear at the end" : "paused · scrolled away from the end"}</span>
          <span className="grow" />
          {!pinned && (
            <button className="copy" onClick={() => setPinned(true)}>
              Follow again
            </button>
          )}
        </div>
      </div>
      {!embedded && (
        <Keys
          keys={[
            ["esc", "overview"],
            ["space", "pause"],
            ["/", "filter"],
            ["1-4", "level"],
            ["t", "relative / clock"],
            ["[ ]", "run"],
            ["c", "copy shown"],
          ]}
          note="from Overview: All logs, or L"
        />
      )}
    </>
  );
}

const LEVELS = ["Debug", "Info+", "Warn+", "Errors"];
