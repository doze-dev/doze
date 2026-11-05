import { useEffect } from "react";

// ? as the Help board draws it: the keys that work anywhere, the keys on a
// row, and what the words mean.
export function Help({ version, onClose }: { version: string; onClose: () => void }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      e.stopPropagation();
      if (e.key === "Escape" || e.key === "?") {
        e.preventDefault();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [onClose]);
  return (
    <div className="scrim" onClick={onClose}>
      <section className="hp" role="dialog" aria-label="Help" onClick={(e) => e.stopPropagation()}>
        <div className="hc">
          <h3>Anywhere</h3>
          {ANYWHERE.map(([k, w]) => (
            <div key={k} className="kr">
              <kbd>{k}</kbd>
              <span>{w}</span>
            </div>
          ))}
        </div>
        <div className="hc">
          <h3>On a row or tile</h3>
          {ROW.map(([k, w]) => (
            <div key={k} className="kr">
              <kbd>{k}</kbd>
              <span>{w}</span>
            </div>
          ))}
          <div className="dim" style={{ fontSize: 11.5, marginTop: 4 }}>Letters are off while a console has focus.</div>
        </div>
        <div className="hc">
          <h3>The words</h3>
          {WORDS.map(([w, tone, what]) => (
            <div key={w} className="wd">
              <b className={tone}>{w}</b>
              <span>{what}</span>
            </div>
          ))}
        </div>
        <div className="foot">
          <span>
            Every key here is a doze command too: <span className="mono">doze sleep db</span>, <span className="mono">doze logs api</span>. Scripts and CI use those.
          </span>
          <span className="grow" />
          <span className="dim">doze {version}</span>
          <span>
            <kbd>esc</kbd> close
          </span>
        </div>
      </section>
    </div>
  );
}

const ANYWHERE: Array<[string, string]> = [
  ["⌘K", "search a name, or type a verb and a name"],
  ["⌘0", "Library"],
  ["⌘1–9", "a stack's tab"],
  ["⌘O", "open a folder"],
  ["esc", "up a level: a service or the logs → its stack → the Library"],
  ["⌘[ ⌘]", "back · forward, the way you came"],
  ["⌘W", "close a folder with no doze.hcl; a stack keeps running"],
  ["⌘L", "all logs of the stack"],
  ["⌘,", "settings"],
  ["?", "this"],
];

const ROW: Array<[string, string]> = [
  ["↑↓ j k", "move"],
  ["space", "wake · sleep: the same switch as on a tile"],
  ["w s", "wake · sleep"],
  ["p", "pin awake · unpin"],
  ["r", "restart"],
  ["⏎", "open · a task: run"],
  ["l L", "its logs · all logs"],
  ["y", "copy address"],
  ["/", "filter"],
  ["1 2 3", "Tiles · List · Map"],
];

const WORDS: Array<[string, string, string]> = [
  ["awake", "ok", "running and reachable"],
  ["dozing", "warn", "awake with nothing holding it; counting down to sleep"],
  ["asleep", "dim", "stopped, data kept; the next connection wakes it"],
  ["waking", "cool", "starting because something asked for it"],
  ["pinned", "bright", "never sleeps"],
  ["off", "dim", "not part of this run; nothing wakes it until you switch it on"],
  ["crashing", "bad", "exits soon after starting; doze tries 5 times, then leaves it off"],
  ["needs", "bright", "what it wakes with · held by: what keeps it awake"],
];
