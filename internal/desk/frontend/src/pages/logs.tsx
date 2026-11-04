import { useCallback, useEffect, useRef, useState } from "react";
import { useStore } from "../store";
import { transport, type view } from "../transport";
import { LogList } from "../components/vlist";

const WINDOW = 200;

// Logs: one instance's lines, or every instance's merged, filtered in Go
// by level, run and search; the list is a window the page asks for as it
// scrolls, and a push says when there is more.
export function LogsPage({ stackKey, instance, embedded }: { stackKey: string; instance: string; embedded?: boolean }) {
  const { logsVersion } = useStore();
  const [level, setLevel] = useState(0);
  const [search, setSearch] = useState("");
  const [runIdx, setRunIdx] = useState(0); // 0 = every run; otherwise an index into logs.runs from the newest
  const [logs, setLogs] = useState<view.Logs | null>(null);
  const [pinned, setPinned] = useState(true);
  const want = useRef(0);

  const fetchWindow = useCallback(
    async (from: number) => {
      const t = await transport();
      const runs = logs?.runs ?? [];
      const run = runIdx > 0 ? (runs[runs.length - runIdx] ?? 0) : 0;
      const out = await t.logs(stackKey, { instance, level, search, run, from, n: WINDOW });
      setLogs(out);
    },
    [stackKey, instance, level, search, runIdx, logs?.runs],
  );

  // The newest window on every change; the pin keeps the end in view.
  useEffect(() => {
    void (async () => {
      const t = await transport();
      const runs = logs?.runs ?? [];
      const run = runIdx > 0 ? (runs[runs.length - runIdx] ?? 0) : 0;
      const probe = await t.logs(stackKey, { instance, level, search, run, from: 0, n: 1 });
      const from = pinned ? Math.max(0, probe.total - WINDOW) : want.current;
      setLogs(await t.logs(stackKey, { instance, level, search, run, from, n: WINDOW }));
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stackKey, instance, level, search, runIdx, logsVersion, pinned]);

  const onNeed = useCallback(
    (from: number) => {
      want.current = from;
      void fetchWindow(from);
    },
    [fetchWindow],
  );

  const runs = logs?.runs ?? [];
  return (
    <div className={"logs" + (embedded ? " embedded" : "")}>
      <div className="logs-bar">
        {!embedded && <span className="muted">{instance || "all logs"}</span>}
        <select value={level} onChange={(e) => setLevel(Number(e.target.value))} aria-label="Level">
          {(logs?.levels ?? ["everything", "info and up", "warnings and up", "errors only"]).map((name, i) => (
            <option key={name} value={i}>
              {name}
            </option>
          ))}
        </select>
        <input type="search" placeholder="search" value={search} onChange={(e) => setSearch(e.target.value)} aria-label="Search" />
        {runs.length > 1 && (
          <span className="runs-step">
            <button onClick={() => setRunIdx(Math.min(runIdx + 1, runs.length))} disabled={runIdx >= runs.length} aria-label="Earlier run">
              [
            </button>
            <span className="muted">{runIdx === 0 ? "every run" : `run ${runs[runs.length - runIdx]}`}</span>
            <button onClick={() => setRunIdx(Math.max(runIdx - 1, 0))} disabled={runIdx === 0} aria-label="Later run">
              ]
            </button>
          </span>
        )}
        <span className="muted count">{logs ? `${logs.total} lines` : ""}</span>
        {!pinned && (
          <button className="pin" onClick={() => setPinned(true)}>
            ↓ newest
          </button>
        )}
      </div>
      {logs && <LogList lines={logs.lines ?? []} total={logs.total} from={logs.from} showInstance={instance === ""} onNeed={onNeed} pinned={pinned} onPinned={setPinned} />}
    </div>
  );
}
