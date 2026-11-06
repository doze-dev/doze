import { useEffect, useState } from "react";
import { copy, go, repair, reopens, set, setMode, setReopens, setTheme, useStore, type Mode, type Theme } from "../store";
import { transport, type view } from "../transport";
import { pageKey } from "../keys";
import { Keys } from "../components/keys";

// Settings, as the Settings and Doctor boards lay them out: a short list
// at the left (General, Machine, About) and one section at a time. Only
// what the app honours today is here: the theme, the machine's setup part
// by part with the command that fixes each, and the version. Startup,
// consoles in the tab and the editor arrive with the install flow (P3).
// The swatches draw each colour theme in the mode on screen: its ground and
// its accent, good and cool, as tokens.css has them.
const THEMES: { name: Theme; dark: { bg: string; swatches: string[] }; light: { bg: string; swatches: string[] } }[] = [
  { name: "violet", dark: { bg: "#15131d", swatches: ["#BD93F9", "#7EE787", "#82AAFF"] }, light: { bg: "#fbfaff", swatches: ["#7a4fe0", "#1d8a4a", "#2f62d9"] } },
  { name: "teal", dark: { bg: "#0e1816", swatches: ["#4FD6C4", "#86E08A", "#8FBDFF"] }, light: { bg: "#f6fbfa", swatches: ["#0f7f73", "#1d8a4a", "#2f62d9"] } },
  { name: "burgundy", dark: { bg: "#1a0f13", swatches: ["#F08FA6", "#8FE0A0", "#8DB4F5"] }, light: { bg: "#fdf8f9", swatches: ["#b5345a", "#1d8a4a", "#2f62d9"] } },
  { name: "amber", dark: { bg: "#16140b", swatches: ["#F2E04A", "#9BE07A", "#7FC4F5"] }, light: { bg: "#fcfbf3", swatches: ["#836700", "#3f7d1f", "#2563a8"] } },
  { name: "slate", dark: { bg: "#12161b", swatches: ["#9DB7D5", "#8FD694", "#5FD0E0"] }, light: { bg: "#f8fafc", swatches: ["#3d6a99", "#237a3b", "#0e7490"] } },
];

const SECTIONS: Record<string, string> = { names: "names", domains: "domains", app: "the app" };
const SECTION_NOTES: Record<string, string> = { names: "shared with doze-aws and doze-kafka", domains: "declared by stacks on this Mac", app: "" };

export function SettingsPage({ library, version, section }: { library: view.Library | null; version: string; section?: string }) {
  const theme = useStore((s) => s.theme);
  const mode = useStore((s) => s.mode);
  const dark = useStore((s) => s.dark);
  const [part, setPart] = useState(section ?? "general");
  useEffect(() => {
    if (!library) void go({ key: "", page: "settings", section });
  }, [library]);
  useEffect(() => {
    if (section) setPart(section);
  }, [section]);
  // The part shown is part of where the page is, so back and forward
  // return to it.
  const show = (p: string) => {
    setPart(p);
    set((s) => (s.where?.page === "settings" ? { where: { ...s.where, section: p } } : {}));
  };
  const checks = library?.doctor ?? [];
  const todo = checks.filter((c) => !c.ok).length;

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!pageKey(e)) return;
      if (e.key === "c" && part === "machine") void copyReport(checks);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [part, checks]);

  return (
    <div className="sets">
      <aside className="side" aria-label="Settings">
        <h4>Settings</h4>
          <button className={"si" + (part === "general" ? " on" : "")} onClick={() => show("general")}>
            <span className="t">General</span>
          </button>
          <button className={"si" + (part === "machine" ? " on" : "")} onClick={() => show("machine")}>
            <span className="t">Machine</span>
            <small className={todo ? "warn" : ""}>{todo ? `${todo} to do` : "all good"}</small>
          </button>
          <button className={"si" + (part === "about" ? " on" : "")} onClick={() => show("about")}>
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
                Mode
                <small>{mode === "system" ? `following macOS: ${dark ? "dark" : "light"} now` : "the same whatever macOS does"}</small>
              </div>
              <div className="v">
                <div className="seg" role="radiogroup" aria-label="Mode" style={{ alignSelf: "flex-start" }}>
                  {(["system", "light", "dark"] as Mode[]).map((m) => (
                    <button key={m} role="radio" aria-checked={mode === m} className={mode === m ? "on" : ""} onClick={() => void setMode(m)}>
                      {m === "system" ? "System" : m === "light" ? "Light" : "Dark"}
                    </button>
                  ))}
                </div>
              </div>
              <div className="k">
                Colour theme
                <small>the terminal&apos;s five, each in light and dark</small>
              </div>
              <div className="v">
                <div className="swatches" role="radiogroup" aria-label="Colour theme">
                  {THEMES.map((t) => {
                    const look = dark ? t.dark : t.light;
                    return (
                      <button key={t.name} role="radio" aria-checked={theme === t.name} className={"swatch" + (theme === t.name ? " on" : "")} onClick={() => void setTheme(t.name)}>
                        <div className="pv" style={{ background: look.bg }}>
                          {look.swatches.map((c, i) => (
                            <i key={c} style={{ background: c, width: ["70%", "40%", "55%"][i] }} />
                          ))}
                        </div>
                        <div className="nm">{t.name}</div>
                      </button>
                    );
                  })}
                </div>
              </div>
              <div className="k">
                Startup
                <small>the bat is light; the window is not kept</small>
              </div>
              <div className="v">
                <button className="radio" role="switch" aria-checked={!!library?.openAtLogin} onClick={() => transport().then((t) => t.openAtLogin(!library?.openAtLogin))}>
                  <span className={"sw" + (library?.openAtLogin ? " on" : "")} aria-hidden />
                  Open at login, in the menu bar only
                </button>
                <ReopenSwitch />
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

/** ReopenSwitch is "reopen the page I had": kept by the page itself, since only it knows where it was. */
function ReopenSwitch() {
  const [on, setOn] = useState(reopens());
  return (
    <button
      className="radio"
      role="switch"
      aria-checked={on}
      onClick={() => {
        setReopens(!on);
        setOn(!on);
      }}
    >
      <span className={"sw" + (on ? " on" : "")} aria-hidden />
      Reopen the page I had, when the window opens
    </button>
  );
}
