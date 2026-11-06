import { useState } from "react";
import { copy } from "../store";
import type { view } from "../transport";

// A card's key/value rows with a Copy at the right (the boards' .kv and
// .copy): the connect card, a task's reads, a crash's env.
export function KVRows({ rows }: { rows: view.KV[] }) {
  return (
    <>
      {rows.map((r) => (
        <div key={r.k} className="kv">
          <span className="k">{r.k}</span>
          <span className={"v" + (r.mono ? " mono" : "") + (r.dim ? " dim" : " bright")}>{r.v}</span>
          {r.copy ? <Copy text={r.copy} label={r.k === "Env var" ? "Copy export" : "Copy"} /> : <span />}
          {r.note && <span className="note">{r.note}</span>}
        </div>
      ))}
    </>
  );
}

export function Copy({ text, label = "Copy" }: { text: string; label?: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      className="copy"
      onClick={() => {
        void copy(text, label === "Copy export" ? "the export line" : "the value");
        setDone(true);
        setTimeout(() => setDone(false), 1200);
      }}
    >
      {done ? "Copied" : label}
    </button>
  );
}
