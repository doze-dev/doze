import { useEffect, useRef } from "react";
import { useTrap } from "./trap";

// ? as the Help board draws it: the keys that work anywhere, the keys on a
// row, and what the words mean.
export function Help({ version, onClose }: { version: string; onClose: () => void }) {
  const box = useRef<HTMLElement>(null);
  useTrap(box);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Tab") return;
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
      <section ref={box} className="hp" role="dialog" aria-modal="true" aria-label="Help" tabIndex={-1} onClick={(e) => e.stopPropagation()}>
        <div className="hc">
          <h3>Anywhere</h3>
          {ANYWHERE.map(([k, w]) => (
            <div key={k} className="kr">
              <kbd>{k}</kbd>
              <span>{w}</span>
            </div>
          ))}
          <Section title={HOME[0]} keys={HOME[1]} />
        </div>
        <div className="hc">
          <h3>On a row or tile</h3>
          {ROW.map(([k, w]) => (
            <div key={k} className="kr">
              <kbd>{k}</kbd>
              <span>{w}</span>
            </div>
          ))}
          <div className="dim" style={{ fontSize: 11.5, margin: "2px 0 4px" }}>Letters are off while a console has focus.</div>
          {MORE.map(([title, keys]) => (
            <Section key={title} title={title} keys={keys} />
          ))}
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
  ["⌘0", "Home"],
  ["⌘1–9", "a stack's tab"],
  ["⌘O", "open a folder"],
  ["esc", "up a level: a service or the logs → its stack → Home"],
  ["⌘[ ⌘]", "back · forward, the way you came"],
  ["⌘W", "close the window; the stacks keep running"],
  ["⌘L", "all logs of the stack"],
  ["⌘⇧N", "Needs you: what is broken or worth doing"],
  ["⌘,", "settings"],
  ["? ⌘/", "this sheet"],
];

function Section({ title, keys }: { title: string; keys: Array<[string, string]> }) {
  return (
    <>
      <h3 style={{ marginTop: 4 }}>{title}</h3>
      {keys.map(([k, w]) => (
        <div key={k} className="kr">
          <kbd>{k}</kbd>
          <span>{w}</span>
        </div>
      ))}
    </>
  );
}

const HOME: [string, Array<[string, string]>] = [
  "Home",
  [
    ["space", "bring a stack up · down"],
    ["u", "up as last time"],
    ["c", "choose what starts"],
    ["⌫", "remove from doze (files stay)"],
  ],
];

const MORE: Array<[string, Array<[string, string]>]> = [
  [
    "Logs",
    [
      ["space", "pause · follow"],
      ["1–4", "level"],
      ["[ ]", "earlier · later run"],
      ["t", "relative · clock time"],
      ["c", "copy what is shown"],
    ],
  ],
  [
    "A crashing service",
    [
      ["r", "try now"],
      ["s", "stop trying"],
    ],
  ],
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
  ["1 2 3 4", "Tiles · List · Map · Grid"],
];

const WORDS: Array<[string, string, string]> = [
  ["awake", "ok", "running, and something used it lately"],
  ["dozing", "warn", "running, quiet for a while; sleeps when its timer runs out"],
  ["waking", "cool", "starting, because something asked for it"],
  ["asleep", "dim", "not running, data kept; the next connection wakes it"],
  ["crashing", "bad", "exits soon after starting; doze tries again, a few times"],
  ["failed", "bad", "doze stopped trying; it waits for you"],
  ["off", "dim", "turned off: nothing wakes it until you switch it on"],
  ["paused", "dim", "enabled = false in doze.hcl; only the file brings it back"],
  ["pinned", "bright", "never sleeps, whatever holds it"],
  ["stopped", "dim", "the whole stack is down; nothing runs"],
  ["the switch", "bright", "on while it is up or on its way; flip it to wake or sleep"],
];
