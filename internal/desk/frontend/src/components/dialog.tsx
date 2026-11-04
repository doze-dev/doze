import { useEffect, useState } from "react";
import type { view } from "../transport";

// A confirmation over the dimmed window, as the Confirm, Dialogs and Reset
// boards draw it, in the dash's words: the question, who is using it, what
// happens, the buttons in the dash's order, the footer. Enter presses the
// default, Escape cancels, ←→ move between buttons; a reset waits for the
// name to be typed and defaults to Cancel.
export function Dialog({ d, onChoose, onCancel }: { d: view.Dialog; onChoose: (b: view.Button, text: string) => void; onCancel: () => void }) {
  const buttons = d.buttons ?? [];
  const [at, setAt] = useState(d.default);
  const [typed, setTyped] = useState("");
  // A reset waits for the name; an action with input waits for a line.
  const armed = d.typeName ? typed === d.typeName : d.input ? typed.trim() !== "" : true;
  const text = d.input ? typed : "";
  const enabled = (b: view.Button) => b.cancel || armed || (!b.danger && !d.input);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // The dialog owns the keyboard: nothing reaches the page beneath.
      e.stopPropagation();
      switch (e.key) {
        case "Escape":
          e.preventDefault();
          onCancel();
          break;
        case "ArrowRight":
        case "Tab":
          e.preventDefault();
          setAt((i) => (i + (e.shiftKey ? buttons.length - 1 : 1)) % buttons.length);
          break;
        case "ArrowLeft":
          e.preventDefault();
          setAt((i) => (i + buttons.length - 1) % buttons.length);
          break;
        case "Enter": {
          const b = buttons[at];
          if (b && enabled(b)) {
            e.preventDefault();
            b.cancel ? onCancel() : onChoose(b, text);
          }
          break;
        }
        case "y":
          if (d.kind === "restart" && !d.typeName) buttons.filter((b) => !b.cancel)[0] && onChoose(buttons.filter((b) => !b.cancel)[0], "");
          break;
        case "n":
          if (d.kind === "restart") onCancel();
          break;
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [at, buttons, armed, d]);

  return (
    <div className="scrim" onClick={onCancel}>
      <section className={"dialog " + d.kind + (d.danger ? " danger" : "")} role="dialog" aria-label={d.title} onClick={(e) => e.stopPropagation()}>
        <h2>{d.danger ? <span className="bad">{d.title}</span> : d.title}</h2>
        {(d.sentence ?? []).length > 0 && (
          <div className="say">
            {(d.sentence ?? []).map((s, i) => (s.strong ? <b key={i} className="bright">{s.text}</b> : <span key={i}>{s.text}</span>))}
          </div>
        )}
        {(d.rows ?? []).map((r, i) =>
          r.k ? (
            <div key={r.k} className="row2">
              <span className="k">{r.k}</span>
              <span>{r.v}</span>
            </div>
          ) : (
            <div key={i} className="say">
              {r.v}
            </div>
          ),
        )}
        {d.input && <input className="field" autoFocus aria-label={d.hint} placeholder={d.hint} value={typed} onChange={(e) => setTyped(e.target.value)} />}
        {(d.facts ?? []).length > 0 && (
          <div className="lines">
            {(d.facts ?? []).map((f) => (
              <div key={f.k} style={{ display: "contents" }}>
                <span className="k">{f.k}</span>
                <span className={"tone-" + f.tone}>
                  {(f.spans ?? []).map((s, i) =>
                    s.strong || s.mono ? (
                      <b key={i} className={(s.mono ? "mono " : "") + "bright"}>
                        {s.text}
                      </b>
                    ) : (
                      <span key={i}>{s.text}</span>
                    ),
                  )}
                </span>
              </div>
            ))}
          </div>
        )}
        {d.typeName && (
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            <span>
              Type <b className="mono bright">{d.typeName}</b> to confirm
            </span>
            <input className="typed" autoFocus aria-label={`Type ${d.typeName} to confirm`} value={typed} onChange={(e) => setTyped(e.target.value)} />
          </div>
        )}
        <div className="btns">
          {buttons.map((b, i) => (
            <button
              key={b.label}
              className={"btn" + (b.primary ? " pri" : "") + (b.danger ? " danger" : "") + (i === at && !b.primary ? " ring2" : "") + (!enabled(b) ? " off" : "")}
              disabled={!enabled(b)}
              onClick={() => (b.cancel ? onCancel() : onChoose(b, text))}
              onMouseEnter={() => setAt(i)}
            >
              {b.label}
              {b.cancel ? <kbd>esc</kbd> : i === at && <kbd>⏎</kbd>}
            </button>
          ))}
        </div>
        {(d.foot ?? []).length > 0 && (
          <div className="foot">
            {(d.foot ?? []).map((f, i) => (
              <span key={i}>{f.startsWith("From a script: ") ? <>From a script: <span className="mono bright">{f.slice("From a script: ".length)}</span></> : f}</span>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
