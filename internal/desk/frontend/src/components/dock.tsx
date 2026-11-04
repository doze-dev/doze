import { useEffect, useRef, useState } from "react";
import { useStore } from "../store";
import { transport, type view } from "../transport";

// The dock under home's table (the Main board's .dock): the selected
// instance's last lines, following. Go keeps the lines; a push says when
// there are more.
export function Dock({ stackKey, instance, onAllLogs }: { stackKey: string; instance: string; onAllLogs: () => void }) {
  const logsVersion = useStore((s) => s.logsVersion);
  const [logs, setLogs] = useState<view.Logs | null>(null);
  const end = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!instance) return;
    let gone = false;
    transport().then(async (t) => {
      const probe = await t.logs(stackKey, { instance, level: 0, from: 0, n: 1 });
      const out = await t.logs(stackKey, { instance, level: 0, from: Math.max(0, probe.total - 60), n: 60 });
      if (!gone) setLogs(out);
    });
    return () => {
      gone = true;
    };
  }, [stackKey, instance, logsVersion]);
  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" });
  }, [logs]);
  return (
    <div className="dock">
      <div className="cap">
        <b>{instance}</b>
        <span>logs · following</span>
        <span className="grow" />
        <button className="copy" onClick={onAllLogs}>
          All logs <kbd>L</kbd>
        </button>
      </div>
      <div className="log mono">
        {(logs?.lines ?? []).map((l, i) => (
          <div key={i} className="ln">
            <span className="dim">{l.time}</span>
            <span className={"lv " + tone(l.rank)}>{l.level}</span>
            <span>{l.line}</span>
          </div>
        ))}
        {logs && logs.total === 0 && <div className="dim">nothing printed yet</div>}
        <div ref={end} />
      </div>
    </div>
  );
}

export function tone(rank: number): string {
  return rank >= 3 ? "bad" : rank === 2 ? "warn" : rank === 0 ? "dim" : "cool";
}
