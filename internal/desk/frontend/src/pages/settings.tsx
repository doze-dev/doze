import { useEffect, useState } from "react";
import { copy, go, repair, setTheme, useStore, type Theme } from "../store";
import { transport, type view } from "../transport";
import { Keys } from "../components/keys";

// Settings, as the Settings and Doctor boards lay them out: a short list
// at the left (General, Machine, About) and one section at a time. Only
// what the app honours today is here: the theme, the machine's setup part
// by part with the command that fixes each, and the version. Startup,
// consoles in the tab and the editor arrive with the install flow (P3).
const THEMES: { name: string; bg: string; swatches: string[] }[] = [
  { name: "violet", bg: "#15131d", swatches: ["#BD93F9", "#7EE787", "#82AAFF"] },
  { name: "teal", bg: "#0e1816", swatches: ["#4FD6C4", "#86E08A", "#8FBDFF"] },
  { name: "burgundy", bg: "#1a0f13", swatches: ["#F08FA6", "#8FE0A0", "#8DB4F5"] },
  { name: "amber", bg: "#16140b", swatches: ["#F2E04A", "#9BE07A", "#7FC4F5"] },
  { name: "slate", bg: "#12161b", swatches: ["#9DB7D5", "#8FD694", "#5FD0E0"] },
  { name: "light", bg: "#fbfaff", swatches: ["#7a4fe0", "#1d8a4a", "#2f62d9"] },
];

const SECTIONS: Record<string, string> = { names: "names", domains: "domains", app: "the app" };
const SECTION_NOTES: Record<string, string> = { names: "shared with doze-aws and doze-kafka", domains: "declared by stacks on this Mac", app: "" };

export function SettingsPage({ library, version, section }: { library: view.Library | null; version: string; section?: string }) {
  const theme = useStore((s) => s.theme);
  const [part, setPart] = useState(section ?? "general");
  useEffect(() => {
    if (!library) void go({ key: "", page: "settings" });
  }, [library]);
  useEffect(() => {
    if (section) setPart(section);
  }, [section]);
  const checks = library?.doctor ?? [];
  const todo = checks.filter((c) => !c.ok).length;

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const tag = (e.target as HTMLElement).tagName;
      if (tag === "INPUT" || tag === "TEXTAREA") return;
      if (e.key === "c" && part === "machine") void copyReport(checks);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [part, checks]);

  return (
    <div className="sets">
      <aside className="side" aria-label="Settings">
        <h4>Settings</h4>
          <button className={"si" + (part === "general" ? " on" : "")} onClick={() => setPart("general")}>
            <span className="t">General</span>
          </button>
          <button className={"si" + (part === "machine" ? " on" : "")} onClick={() => setPart("machine")}>
            <span className="t">Machine</span>
            <small className={todo ? "warn" : ""}>{todo ? `${todo} to do` : "all good"}</small>
          </button>
          <button className={"si" + (part === "about" ? " on" : "")} onClick={() => setPart("about")}>
            <span className="t">About</span>
            <small>{version}</small>
          </button>
        <div className="foot">doze {version}</div>
      </aside>
      <div className="main">
        {part === "general" && (
          <>
            <div className="head">
              <h1>General</h1>
              <span className="path">saved as you change them · ~/.doze/desk/prefs.json</span>
            </div>
            <div className="set">
              <div className="k">
                Theme
                <small>the five from the terminal, and a light one</small>
              </div>
              <div className="v">
                <div className="swatches" role="radiogroup" aria-label="Theme">
                  {THEMES.map((t) => (
                    <button key={t.name} role="radio" aria-checked={theme === t.name} className={"swatch" + (theme === t.name ? " on" : "")} onClick={() => void setTheme(t.name as Theme)}>
                      <div className="pv" style={{ background: t.bg }}>
                        {t.swatches.map((c, i) => (
                          <i key={c} style={{ background: c, width: ["70%", "40%", "55%"][i] }} />
                        ))}
                      </div>
                      <div className="nm">{t.name}</div>
                    </button>
                  ))}
                </div>
                <button className="radio" onClick={() => void setTheme("system")}>
                  <span className={"o" + (theme === "system" ? " on" : "")} />
                  Follow the system: light when macOS is light, violet otherwise
                </button>
                <button className="radio" onClick={() => void setTheme(theme === "system" ? "violet" : theme)}>
                  <span className={"o" + (theme !== "system" ? " on" : "")} />
                  Always this one
                </button>
              </div>
            </div>
          </>
        )}
        {part === "machine" && (
          <>
            <div className="head">
              <h1>Machine</h1>
              <span className="path">what doze set up here, part by part</span>
              <span className="grow" />
              <button className="btn" onClick={() => void copyReport(checks)}>
                Copy report <kbd>c</kbd>
              </button>
            </div>
            {todo > 0 ? (
              <div className="banner warn" role="status">
                <span className="dot y" aria-hidden />
                <div className="t">
                  <b>
                    {todo} part{todo === 1 ? " is" : "s are"} not in place. Each row below says what puts it there.
                  </b>
                  <span>{library?.repairable || library?.repairing ? "Repair runs the same setup as doze doctor --fix, behind the system's password prompt." : "From a terminal: doze doctor --fix, one sudo; each row's fix is the command."}</span>
                </div>
                {(library?.repairable || library?.repairing) && (
                  <button className="btn pri" disabled={!!library?.repairing} onClick={() => void repair()}>
                    {library?.repairing ? "Repairing…" : "Repair"} {!library?.repairing && <span className="dim">(one password)</span>}
                  </button>
                )}
              </div>
            ) : (
              <div className="banner" role="status">
                <span className="dot g" aria-hidden />
                <div className="t">
                  <b>Everything is in place.</b>
                  <span>The same report as doze doctor.</span>
                </div>
              </div>
            )}
            <Doctor checks={checks} />
            <p className="model">The same report as <span className="mono">doze doctor</span>, with the fixes attached.</p>
          </>
        )}
        {part === "about" && (
          <>
            <div className="head">
              <h1>About</h1>
              <span className="path">doze {version}</span>
            </div>
            <div className="set">
              <div className="k">
                doze
                <small>the app and the CLI are one binary</small>
              </div>
              <div className="v">
                <span className="mono">{version}</span>
                <button className="btn" style={{ alignSelf: "flex-start" }} onClick={() => transport().then((t) => t.open("https://doze.dev"))}>
                  doze.dev
                </button>
              </div>
            </div>
          </>
        )}
        <Keys keys={part === "machine" ? [["c", "copy report"], ["esc", "back"]] : [["⌘,", "here"], ["esc", "back"]]} note="from the menu bar, the Library's Machine line, or ⌘," />
      </div>
    </div>
  );
}

function Doctor({ checks }: { checks: view.Check[] }) {
  const sections = Array.from(new Set(checks.map((c) => c.section ?? "")));
  return (
    <div className="chk" role="table" aria-label="Machine">
      {sections.map((s) => (
        <div key={s} style={{ display: "contents" }}>
          <div className="cr sec">
            {SECTIONS[s] ?? s}
            <span>{SECTION_NOTES[s] ?? ""}</span>
          </div>
          {checks
            .filter((c) => (c.section ?? "") === s)
            .map((c) => (
              <div key={c.name} className="cr">
                <span className={"dot " + (c.ok ? "g" : "y")} aria-hidden />
                <b>{c.name}</b>
                <span className="d" title={c.text}>
                  {c.text}
                </span>
                {!c.ok && c.fix ? (
                  <button className="copy" onClick={() => void copy(c.fix ?? "", "the command")} title={c.fix}>
                    Copy fix
                  </button>
                ) : (
                  <span />
                )}
              </div>
            ))}
        </div>
      ))}
    </div>
  );
}

async function copyReport(checks: view.Check[]) {
  const text = checks.map((c) => `${c.ok ? "✓" : "✗"} ${c.section ? c.section + " · " : ""}${c.name}${c.text ? " · " + c.text : ""}${!c.ok && c.fix ? "   fix: " + c.fix : ""}`).join("\n");
  await copy(text, "the report");
}
