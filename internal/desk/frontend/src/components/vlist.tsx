import { useEffect, useRef, type ReactNode } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { view } from "../transport";
import { tone } from "./dock";

// Log lines, virtualised: only the rows on screen exist in the DOM. The
// list stays pinned to the newest line until the user scrolls up. Drawn as
// the Logs board draws them: time, the instance in its own colour on the
// merged page, the level tag, the text with matches highlighted.
export function LogList({
  lines,
  total,
  from,
  merged,
  instances,
  clock,
  search,
  onNeed,
  pinned,
  onPinned,
}: {
  lines: view.Line[];
  total: number;
  from: number;
  merged: boolean;
  instances: string[];
  clock: boolean;
  search: string;
  /** onNeed asks for a window starting at an index the list scrolled to. */
  onNeed: (from: number) => void;
  pinned: boolean;
  onPinned: (pinned: boolean) => void;
}) {
  const parent = useRef<HTMLDivElement>(null);
  const v = useVirtualizer({
    count: total,
    getScrollElement: () => parent.current,
    estimateSize: () => 24,
    overscan: 20,
  });
  const items = v.getVirtualItems();

  useEffect(() => {
    if (pinned && total > 0) v.scrollToIndex(total - 1, { align: "end" });
  }, [total, pinned, v]);

  useEffect(() => {
    if (items.length === 0) return;
    const first = items[0].index;
    const last = items[items.length - 1].index;
    if (first < from || last >= from + lines.length) onNeed(Math.max(0, first - 40));
  }, [items, from, lines.length, onNeed]);

  const onScroll = () => {
    const el = parent.current;
    if (!el) return;
    const atEnd = el.scrollTop + el.clientHeight >= el.scrollHeight - 4;
    if (atEnd !== pinned) onPinned(atEnd);
  };

  const colour = (name: string) => "c" + (Math.max(instances.indexOf(name), 0) % 6);

  return (
    <div className="lg" ref={parent} onScroll={onScroll} role="log" aria-live="off">
      <div style={{ height: v.getTotalSize(), position: "relative" }}>
        {items.map((it) => {
          const l = lines[it.index - from];
          return (
            <div key={it.key} className={"ln mono" + (merged ? " merged" : "")} style={{ position: "absolute", top: 0, transform: `translateY(${it.start}px)`, minHeight: it.size, width: "100%" }}>
              {l ? (
                <>
                  <span className="dim">{clock ? l.time : l.rel}</span>
                  {merged && <span className={"who " + (l.doze ? "acc" : "tone-" + colour(l.instance ?? ""))}>{l.instance}</span>}
                  <span className={"lv " + (l.doze ? "acc" : tone(l.rank))}>{l.level}</span>
                  <span className={l.rank >= 3 ? "bad" : l.rank === 2 ? "warn" : ""}>{highlight(l.line, search)}</span>
                </>
              ) : (
                <span className="dim">…</span>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function highlight(text: string, needle: string) {
  if (!needle) return text;
  const lower = text.toLowerCase();
  const n = needle.toLowerCase();
  const out: ReactNode[] = [];
  let i = 0;
  let k = 0;
  for (;;) {
    const j = lower.indexOf(n, i);
    if (j < 0) break;
    out.push(text.slice(i, j));
    out.push(
      <span key={k++} className="hl">
        {text.slice(j, j + n.length)}
      </span>,
    );
    i = j + n.length;
  }
  out.push(text.slice(i));
  return out;
}
