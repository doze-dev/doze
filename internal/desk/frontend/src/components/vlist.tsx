import { useEffect, useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { view } from "../transport";

// Log lines, virtualised: only the rows on screen exist in the DOM. The
// list stays pinned to the newest line until the user scrolls up.
export function LogList({
  lines,
  total,
  from,
  showInstance,
  onNeed,
  pinned,
  onPinned,
}: {
  lines: view.Line[];
  total: number;
  from: number;
  showInstance: boolean;
  /** onNeed asks for a window starting at an index the list scrolled to. */
  onNeed: (from: number) => void;
  pinned: boolean;
  onPinned: (pinned: boolean) => void;
}) {
  const parent = useRef<HTMLDivElement>(null);
  const v = useVirtualizer({
    count: total,
    getScrollElement: () => parent.current,
    estimateSize: () => 20,
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

  return (
    <div className="vlist" ref={parent} onScroll={onScroll} role="log" aria-live="off">
      <div style={{ height: v.getTotalSize(), position: "relative" }}>
        {items.map((it) => {
          const l = lines[it.index - from];
          return (
            <div key={it.key} className={"line" + (l ? " rank-" + l.rank : "")} style={{ position: "absolute", top: 0, transform: `translateY(${it.start}px)`, height: it.size, width: "100%" }}>
              {l ? (
                <>
                  <span className="ago muted">{l.ago}</span>
                  {showInstance && <span className="who">{l.instance}</span>}
                  {l.level && <span className="level">{l.level}</span>}
                  <span className="text">{l.line}</span>
                </>
              ) : (
                <span className="muted">…</span>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
