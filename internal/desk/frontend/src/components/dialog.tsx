import { useEffect, useRef, useState } from "react";
import type { view } from "../transport";
import { useTrap } from "./trap";

// A confirmation over the dimmed window, as the ConfirmSleep and Reset
// boards draw it, in Go's words: the question, who is using it, what
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
  // Typing the name moves the default onto the reset, as the dash did: ⏎
  // then resets. Before that, and once the name is wrong again, Cancel.
  useEffect(() => {
    if (!d.typeName) return;
    setAt(armed ? Math.max(0, buttons.findIndex((b) => !b.cancel)) : d.default);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [armed]);
  const enabled = (b: view.Button) => b.cancel || armed || (!b.danger && !d.input);
  const box = useRef<HTMLElement>(null);
  useTrap(box);
  // The highlighted button is the focused one, so Tab, the arrows and a
  // screen reader agree; a dialog that asks for text keeps the box.
  useEffect(() => {
    if (d.input || d.typeName) return;
    box.current?.querySelectorAll<HTMLButtonElement>(".acts button")[at]?.focus();
  }, [at, d.input, d.typeName]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // The dialog owns the keyboard: nothing reaches the page beneath,
      // except Tab, which the focus trap moves within the dialog.
      if (e.key === "Tab") return;
      e.stopPropagation();
      switch (e.key) {
        case "Escape":
          e.preventDefault();
          onCancel();
          break;
        case "ArrowRight":
          e.preventDefault();
          setAt((i) => (i + 1) % buttons.length);
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
      <section ref={box} className={"dialog " + d.kind + (d.danger ? " danger" : "")} role="dialog" aria-modal="true" aria-label={d.title} onClick={(e) => e.stopPropagation()}>
        <h2>{d.title}</h2>
        {(d.sentence ?? []).length > 0 && <p>{(d.sentence ?? []).map((s, i) => (s.strong ? <b key={i} className="bright">{s.text}</b> : <span key={i}>{s.text}</span>))}</p>}
        {(d.rows ?? []).length > 0 && (
          <div className="lib" style={{ margin: 0, background: "var(--well)" }}>
            {(d.rows ?? []).map((r, i) =>
              r.k ? (
                <div key={r.k} className="lrow" style={{ gridTemplateColumns: "10px 120px minmax(0,1fr)", padding: "8px 12px" }}>
                  <span className="dot g" aria-hidden />
                  <b className="bright">{r.k}</b>
                  <span className="dim" style={{ whiteSpace: "normal" }}>{r.v}</span>
                </div>
              ) : (
                <div key={i} className="lrow" style={{ gridTemplateColumns: "minmax(0,1fr)", padding: "8px 12px" }}>
                  <span className="dim">{r.v}</span>
                </div>
              ),
            )}
          </div>
        )}
        {d.input && (
          <label className="input">
            <input autoFocus aria-label={d.hint} placeholder={d.hint} value={typed} onChange={(e) => setTyped(e.target.value)} />
          </label>
        )}
        {(d.facts ?? []).length > 0 && (
          <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
            {(d.facts ?? []).map((f) => (
              <div key={f.k} className="kv" style={{ gridTemplateColumns: "110px minmax(0,1fr)" }}>
                <span className="k">{f.k}</span>
                <span className={"tone-" + f.tone} style={{ whiteSpace: "normal" }}>
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
            <span className="dim" style={{ fontSize: 12.5 }}>
              Type <b className="mono bright">{d.typeName}</b> to confirm
            </span>
            <label className="input">
              <input autoFocus aria-label={`Type ${d.typeName} to confirm`} value={typed} onChange={(e) => setTyped(e.target.value)} className="mono" />
            </label>
          </div>
        )}
        {(d.foot ?? []).length > 0 && (
          <p className="dim" style={{ fontSize: 12 }}>
            {(d.foot ?? []).map((f, i) => (
              <span key={i}>
                {f.startsWith("From a script: ") ? (
                  <>
                    From a script: <span className="mono bright">{f.slice("From a script: ".length)}</span>
                  </>
                ) : (
                  f
                )}{" "}
              </span>
            ))}
          </p>
        )}
        <div className="acts">
          {buttons.map((b, i) => (
            <button
              key={b.label}
              className={"btn" + (b.primary ? " pri" : "") + (b.danger ? " danger" : "")}
              style={i === at && !b.primary && !b.danger ? { borderColor: "var(--accent)" } : undefined}
              disabled={!enabled(b)}
              onClick={() => (b.cancel ? onCancel() : onChoose(b, text))}
              onMouseEnter={() => setAt(i)}
              onFocus={() => setAt(i)}
            >
              {b.label}
              {b.cancel ? <kbd>esc</kbd> : i === at && <kbd>⏎</kbd>}
            </button>
          ))}
        </div>
      </section>
    </div>
  );
}
