import { useEffect } from "react";
import { go, run, select, useStore } from "../store";
import type { view } from "../transport";
import { Vitals } from "../components/vitals";
import { Table } from "../components/table";
import { Dock } from "../components/dock";
import { Keys } from "../components/keys";

// Home, as the Main board lays it out: the head with Sleep all and Up, the
// four vitals, the table, the selected instance's logs in the dock, and the
// keys bar. The keys are the TUI's.
export function HomePage({ home, stackKey }: { home: view.Home; stackKey: string }) {
  const { selected } = useStore();
  const rows = (home.groups ?? []).flatMap((g) => g.rows ?? []);
  const row = rows.find((r) => r.name === selected);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const tag = (e.target as HTMLElement).tagName;
      if (tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA") return;
      const i = rows.findIndex((r) => r.name === selected);
      switch (e.key) {
        case "ArrowDown":
          e.preventDefault();
          if (rows[i + 1]) void select(rows[i + 1].name);
          break;
        case "ArrowUp":
          e.preventDefault();
          if (rows[i - 1]) void select(rows[i - 1].name);
          break;
        case "Enter":
          if (row) void go({ key: stackKey, page: "instance", instance: row.name });
          break;
        case "w":
          if (row && !row.awake) void run(stackKey, row.kind === "task" ? "task" : "wake", row.name);
          break;
        case "s":
          if (row && row.awake) void run(stackKey, "sleep", row.name);
          break;
        case "r":
          if (row) void run(stackKey, row.kind === "task" ? "task" : "restart", row.name);
          break;
        case "l":
          if (row) void go({ key: stackKey, page: "logs", instance: row.name });
          break;
        case "L":
          void go({ key: stackKey, page: "logs" });
          break;
        case "y":
          if (row?.addr) void navigator.clipboard?.writeText(row.addr + (row.port ? ":" + row.port : ""));
          break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [rows, row, selected, stackKey]);

  return (
    <>
      <div className={"head" + (home.banner.ok ? "" : " faded")}>
        <h1>{home.stack}</h1>
        <span className="path">{home.dir}</span>
        <span className="grow" />
        <button className="btn" onClick={() => void run(stackKey, "sleep", "")} title="Sleep everything that is awake">
          Sleep all
        </button>
        <button className="btn pri" onClick={() => void run(stackKey, "up", "")} title="Bring the stack up">
          Up
        </button>
      </div>
      <div className={home.banner.ok ? "" : "faded"} style={{ display: "contents" }}>
        <Vitals v={home.vitals} />
        <Table
          groups={home.groups ?? []}
          selected={selected}
          onSelect={(name) => void select(name)}
          onOpen={(name) => void go({ key: stackKey, page: "instance", instance: name })}
        />
        <Dock stackKey={stackKey} instance={selected} onAllLogs={() => void go({ key: stackKey, page: "logs" })} />
      </div>
      <Keys
        keys={[
          ["↑↓", "move"],
          ["⏎", "open"],
          ["w", "wake"],
          ["s", "sleep"],
          ["r", "restart"],
          ["l", "logs"],
          ["y", "copy address"],
        ]}
        note="the same keys as the TUI"
      />
    </>
  );
}
