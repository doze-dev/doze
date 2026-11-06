import { useEffect, useState } from "react";
import { ask, copy, go, run, say, useStore } from "../store";
import { transport, type view } from "../transport";
import { ChartCard } from "../components/chart";
import { KVRows } from "../components/card";
import { Keys } from "../components/keys";
import { pageKey } from "../keys";
import { Dot } from "../components/sidebar";
import { LogsPage } from "./logs";
import { ConsoleTab, Extends } from "../components/console";

// A service's page, as the inside boards lay it out: the head with the
// switch and its verbs, the tabs, and the overview's cards; a module
// asleep says so and offers to wake; a task shows what Run will do, or its
// last run and every run kept. Every word is Go's.
export function InstancePage({ inst, stackKey }: { inst: view.Instance; stackKey: string }) {
  const tabs = inst.tabs ?? [];
  const [tab, setTab] = useState(tabs[0]?.id ?? "logs");
  const current = tabs.some((t) => t.id === tab) ? tab : (tabs[0]?.id ?? "logs");
  const task = inst.kind === "task";
  const stackDir = useStore((s) => s.stacks.find((x) => x.key === stackKey)?.dir);
  const word = inst.look.word;
  // Every state is Go's: the switch (on, off, paused), the word beside it,
  // and whether it is serving (awake or dozing) for the verbs that need that.
  const awake = word === "awake" || word === "dozing";
  const off = !!inst.off;
  const paused = !!inst.paused;
  const on = inst.on;
  const asleep = word === "asleep";
  const crashing = !!inst.crash;
  const url = inst.connect?.find((r) => /^https?:\/\//.test(r.copy ?? ""))?.copy;
  const flip = () => void (off ? run(stackKey, "on", inst.name).then((went) => went && run(stackKey, "wake", inst.name)) : on ? ask(stackKey, inst.name, "sleep") : run(stackKey, "wake", inst.name));

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!pageKey(e)) return;
      // A console tab's letters are its own actions; the page's verbs wait.
      if (current.startsWith("console:") && e.key.length === 1) return;
      switch (e.key) {
        case "r":
          if (task) void run(stackKey, "task", inst.name);
          else if (crashing) void run(stackKey, "restart", inst.name);
          else if (awake) void ask(stackKey, inst.name, "restart");
          break;
        case "s":
          if (task || paused) break;
          if (crashing) void run(stackKey, "off", inst.name);
          else if (awake) void ask(stackKey, inst.name, "sleep");
          break;
        case "w":
          if (!task && !on && !paused) flip();
          break;
        case "p":
          if (!task && !paused && !off) void run(stackKey, "pin", inst.name);
          break;
        case "o":
          if (url && awake) transport().then((t) => t.open(url));
          break;
        case "y": {
          const line = inst.connect?.find((r) => r.k === "URL" || r.k === "Address")?.copy;
          if (line) void copy(line, `the connect line of ${inst.name}`);
          break;
        }
        case "l":
          setTab("logs");
          break;
        case "Enter":
          if (task) void run(stackKey, "task", inst.name);
          break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [stackKey, inst, tabs, current, task, awake, on, off, paused, crashing, url]);

  if (inst.missing) {
    return (
      <div className="empty">
        <h2>Nothing called {inst.name}.</h2>
        <p>{inst.stack} does not declare it any more. The sidebar lists what it does.</p>
      </div>
    );
  }
  const pinned = !!inst.pinned;
  return (
    <>
      <div className={"head" + (inst.banner.ok ? "" : " faded")}>
        {task ? (
          <span className={"tone-" + inst.look.tone} style={{ fontSize: 14 }} aria-hidden>
            {inst.look.glyph}
          </span>
        ) : (
          <Dot look={inst.look} trouble={!!inst.crash} />
        )}
        <h1>{inst.name}</h1>
        <span className="path">{inst.path}</span>
        <span className="grow" />
        {task ? (
          <>
            {(inst.runs ?? []).length > 0 ? (
              <button className="btn" onClick={() => void copyOutput(stackKey, inst.name)} title="The last run's output, as kept here">
                Copy output
              </button>
            ) : (
              <button className="btn" onClick={() => transport().then((t) => t.open("file://" + (stackDir ?? "") + "/doze.hcl"))} title="The task's block, in your editor">
                Edit doze.hcl
              </button>
            )}
            <button className="btn pri" onClick={() => void run(stackKey, "task", inst.name)}>
              {(inst.runs ?? []).length > 0 ? "Run again" : "Run"} <kbd>⏎</kbd>
            </button>
          </>
        ) : (
          <>
            <span className="swl">
              <span className={"tone-" + inst.look.tone}>{word}</span>
              {paused ? (
                <span className="lock" title="enabled = false in doze.hcl: only the file brings it back">
                  ⊘
                </span>
              ) : (
                <button className={"sw" + (on ? " on" : "") + (off ? " dis" : "")} role="switch" aria-checked={on} aria-label={`${inst.name} is ${word}`} title={off ? "Turned off: switch it on to bring it back and wake it" : on ? "Switch off to put it to sleep" : "Switch on to wake it"} onClick={flip} />
              )}
            </span>
            {paused ? (
              <button className="btn" onClick={() => transport().then((t) => t.open("file://" + (stackDir ?? "") + "/doze.hcl"))}>
                Edit doze.hcl
              </button>
            ) : inst.crash ? (
              <>
                <button className="btn" onClick={() => void run(stackKey, "off", inst.name)} title="Take it out of this run while you look">
                  Stop trying <kbd>s</kbd>
                </button>
                <button className="btn pri" onClick={() => void run(stackKey, "restart", inst.name)}>
                  Try now <kbd>r</kbd>
                </button>
              </>
            ) : (
              <>
                {!off && (
                  <button className="btn" onClick={() => void run(stackKey, "pin", inst.name)}>
                    {pinned ? "Unpin" : "Pin awake"} <kbd>p</kbd>
                  </button>
                )}
                {awake && (
                  <button className="btn" onClick={() => void ask(stackKey, inst.name, "restart")}>
                    Restart <kbd>r</kbd>
                  </button>
                )}
                {awake && !url && (
                  <button className="btn" onClick={() => void ask(stackKey, inst.name, "sleep")}>
                    Sleep <kbd>s</kbd>
                  </button>
                )}
                {!on && !off && (
                  <button className="btn pri" onClick={flip}>
                    Wake <kbd>w</kbd>
                  </button>
                )}
                {url && awake && (
                  <button className="btn pri" onClick={() => transport().then((t) => t.open(url))} title={url}>
                    Open <span className="mono" style={{ fontSize: 12 }}>{url.replace(/^https?:\/\//, "")}</span> <kbd>o</kbd>
                  </button>
                )}
              </>
            )}
          </>
        )}
      </div>
      {inst.broken && (
        <div className="banner warn" role="status" style={{ marginTop: 4 }}>
          <span className="dot y" aria-hidden />
          <div className="t">
            <b>{inst.broken}: {inst.stack} keeps running the file it started with.</b>
            <span>Nothing in it is applied until the file loads.</span>
          </div>
          <button className="btn" onClick={() => void go({ key: stackKey, page: "home" })}>
            See why
          </button>
        </div>
      )}
      <nav className="ptabs" role="tablist">
        {tabs.map((t) => (
          <button key={t.id} role="tab" data-tab={t.id} aria-selected={t.id === current} className={"ptab" + (t.id === current ? " on" : "")} onClick={() => setTab(t.id)}>
            {tabTitle(t, task, inst)}
          </button>
        ))}
      </nav>
      {current === "overview" && (task ? <TaskOverview inst={inst} stackKey={stackKey} onRuns={() => setTab("logs")} /> : asleep ? <Asleep inst={inst} stackKey={stackKey} onConsole={() => setTab("web console")} /> : <Overview inst={inst} stackKey={stackKey} />)}
      {current === "logs" && <LogsPage stackKey={stackKey} instance={inst.name} embedded inst={inst} />}
      {current === "last run" && <TaskOverview inst={inst} stackKey={stackKey} onRuns={() => setTab("logs")} />}
      {current === "web console" && <WebConsole inst={inst} stackKey={stackKey} />}
      {current.startsWith("console:") && <ConsoleTab stackKey={stackKey} inst={inst.name} tab={current} title={(inst.tabs ?? []).find((t) => t.id === current)?.title ?? ""} />}
      {!current.startsWith("console:") && (
        <Keys
          keys={
            task
              ? [
                  ["⏎", "run"],
                  ["l", "logs"],
                  ["esc", "up"],
                ]
              : [
                  ["esc", "up"],
                  ["l", "logs"],
                  ["y", "copy address"],
                  ...(url && awake ? ([["o", "open in browser"]] as Array<[string, string]>) : []),
                  ...(crashing
                    ? ([["r", "try now"], ["s", "stop trying"]] as Array<[string, string]>)
                    : awake
                      ? ([["s", "sleep"], ["r", "restart"], ["p", pinned ? "unpin" : "pin"]] as Array<[string, string]>)
                      : on || paused
                        ? []
                        : ([["w", "wake"]] as Array<[string, string]>)),
                ]
          }
          note="from Overview: a row's name, or ⏎"
        />
      )}
    </>
  );
}

function tabTitle(t: view.Tab, task: boolean, inst: view.Instance): string {
  switch (t.id) {
    case "overview":
      return "Overview";
    case "last run":
      return (inst.runs ?? []).length > 0 ? "Last run" : "Overview";
    case "logs":
      return task ? ((inst.runs ?? []).length > 0 ? `Runs · ${(inst.runs ?? []).length}` : "Runs · none yet") : "Logs";
    case "web console":
      return "Console";
  }
  return t.title;
}

function Overview({ inst, stackKey }: { inst: view.Instance; stackKey: string }) {
  const module = inst.kind === "module";
  return (
    <>
      {inst.crash && (
        <div className="banner bad" role="status" style={{ marginTop: 14 }}>
          <span className="dot r" aria-hidden />
          <div className="t">
            <b>{inst.crash.gaveUp ? "It kept exiting soon after starting, so doze stopped trying. It waits for you." : `It exits soon after starting. doze tries again${inst.crash.nextIn ? " in " + inst.crash.nextIn : ""}${inst.crash.tries ? " (" + inst.crash.tries + ")" : ""}, and stops trying if it keeps exiting.`}</b>
            <span>What it said is below. Fix it and press Try now, or stop trying while you look.</span>
          </div>
          {inst.crash.tries && <span className="pill bad">{inst.crash.tries}</span>}
        </div>
      )}
      {inst.crash?.held && <PortClash inst={inst} stackKey={stackKey} />}
      <div className="two">
        <div className="col">
          {inst.crash && (inst.crash.lastRun ?? []).length > 0 && (
            <section className="card" style={{ flex: 1 }}>
              <h3>
                What it said <span className="dim" style={{ textTransform: "none", letterSpacing: 0, fontWeight: 400 }}>· the last lines before it exited</span>
                <button className="copy" onClick={() => void copy([...(inst.crash?.lastRun ?? []).map((l) => `${l.time} ${l.line}`), inst.crash?.error, inst.crash?.where].filter(Boolean).join("\n"), "what it said")}>
                  Copy
                </button>
              </h3>
              <div className="code" style={{ flex: 1 }}>
                {(inst.crash.lastRun ?? []).map((l, i) => (
                  <div key={i}>
                    <span className="c">{l.time}</span> <span className={l.rank >= 3 ? "bad" : l.rank === 2 ? "warn" : ""}>{l.line}</span>
                  </div>
                ))}
                {inst.crash.error && <div className="bad">{inst.crash.error}</div>}
              </div>
            </section>
          )}
          {(inst.connect ?? []).length > 0 && (
            <section className="card">
              <h3>{module ? "Connect" : "Reach it"}</h3>
              <KVRows rows={inst.connect ?? []} />
            </section>
          )}
          {module && <Snippets inst={inst} />}
          <Extends after="connect" inst={inst} stackKey={stackKey} />
          {!inst.crash && (
          <section className="card">
            <h3>{STATE_TITLE[inst.look.word] ?? inst.look.word}</h3>
            {(inst.holders ?? []).map((p) => (
              <div key={p.text} style={{ display: "flex", gap: 10, alignItems: "center" }}>
                <span className="pill">
                  <span className="dot g" aria-hidden /> {p.text}
                </span>
                <span className="dim">{p.explain}</span>
              </div>
            ))}
            {inst.dozing && (
              <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
                <span className="pill">
                  <span className="dot y" aria-hidden /> <span className="warn">sleeps in {inst.dozing.left}</span>
                </span>
                <div className="bar" style={{ flex: 1 }}>
                  <i className="c3" style={{ width: `${inst.dozing.fraction * 100}%` }} />
                </div>
              </div>
            )}
            {(inst.holders ?? []).length === 0 && !inst.dozing && inst.sleeps && <div className="bright">{inst.sleeps}</div>}
            {((inst.holders ?? []).length > 0 || inst.dozing) && <div className="dim">{inst.sleeps}</div>}
            {(inst.needs ?? []).length > 0 && (
              <div className="chips" style={{ marginTop: 4 }}>
                <span className="dim" style={{ fontSize: 12, alignSelf: "center" }}>needs</span>
                {(inst.needs ?? []).map((n) => (
                  <button key={n.name} className="chip" onClick={() => n.known && void go({ key: stackKey, page: "instance", instance: n.name })}>
                    <span className={"glyph tone-" + n.look.tone} aria-hidden>
                      {n.glyph}
                    </span>
                    {n.name}
                  </button>
                ))}
              </div>
            )}
          </section>
          )}
          <Extends after="awake" inst={inst} stackKey={stackKey} />
          <section className="card" style={{ flex: 1 }}>
            <h3>
              Recent <span className="dim" style={{ textTransform: "none", letterSpacing: 0, fontWeight: 400 }}>· this run</span>
            </h3>
            <div className="mono" style={{ display: "grid", gridTemplateColumns: "64px 70px minmax(0,1fr)", columnGap: 10, lineHeight: "21px" }}>
              {(inst.recent ?? []).map((r, i) => (
                <div key={i} style={{ display: "contents" }}>
                  <span className="dim">{r.at}</span>
                  <span className={"tone-" + r.tone}>{r.kind}</span>
                  <span className="ell">{r.text}</span>
                </div>
              ))}
              {(inst.recent ?? []).length === 0 && <div className="dim" style={{ gridColumn: "1 / -1" }}>nothing yet</div>}
            </div>
          </section>
          <Extends after="recent" inst={inst} stackKey={stackKey} />
        </div>
        <div className="col">
          {inst.vitals ? (
            <>
              <ChartCard title="Memory" chart={inst.vitals.ram} color="cool" tall />
              <ChartCard title="CPU" chart={inst.vitals.cpu} color="accent" tall />
              <ChartCard title="Connections" chart={inst.vitals.conns} color="good" />
              <Extends after="vitals" inst={inst} stackKey={stackKey} />
            </>
          ) : (
            <section className="card">
              <h3>Vitals</h3>
              <div className="dim">{inst.look.word === "disabled" ? "Off: nothing runs, nothing is measured." : "no readings yet"}</div>
            </section>
          )}
        </div>
      </div>
    </>
  );
}

/** Snippets is the module's connect line in the language you are in: the same URL, rendered seven ways. */
function Snippets({ inst }: { inst: view.Instance }) {
  const urlRow = inst.connect?.find((r) => r.k === "URL") ?? inst.connect?.find((r) => r.k === "Address");
  const envRow = inst.connect?.find((r) => r.k === "Env var");
  const url = urlRow?.copy ?? urlRow?.v ?? "";
  const env = (envRow?.v ?? "").split(/[= ]/)[0] || "URL";
  const [lang, setLang] = useState("Node");
  if (!url) return null;
  const langs: Record<string, string> = {
    Node: `// ${env} is already set when doze starts your app.\n// From a terminal: eval "$(doze env)"\nconst url = process.env.${env} ?? "${url}"`,
    Python: `import os\n# ${env} is already set when doze starts your app.\nurl = os.environ.get("${env}", "${url}")`,
    Go: `// ${env} is already set when doze starts your app.\nurl := cmp.Or(os.Getenv("${env}"), "${url}")`,
    Shell: `export ${env}=${url}\n# or everything at once: eval "$(doze env)"`,
    ".env": `${env}=${url}`,
  };
  if (/^postgres/.test(url)) langs.psql = `psql "${url}"`;
  if (/^redis/.test(url)) langs["redis-cli"] = `redis-cli -u "${url}"`;
  const names = Object.keys(langs);
  const code = langs[lang] ?? langs[names[0]];
  return (
    <section className="card" style={{ gap: 10 }}>
      <h3>
        From your code <span className="dim" style={{ textTransform: "none", letterSpacing: 0, fontWeight: 400 }}>· the same URL, in the language you are in</span>
      </h3>
      <div className="ctabs" role="tablist" aria-label="Language">
        {names.map((n) => (
          <button key={n} role="tab" aria-selected={n === lang} className={n === lang ? "on" : ""} onClick={() => setLang(n)}>
            {n}
          </button>
        ))}
      </div>
      <div className="code">
        {code}
        <button className="copybtn" onClick={() => void copy(code, `the ${lang} line`)}>
          Copy
        </button>
      </div>
      <div className="dim" style={{ fontSize: 12 }}>Inside the stack there is nothing to configure: the name resolves and the variable is there. The first connection wakes it if it is asleep.</div>
    </section>
  );
}

function Asleep({ inst, stackKey, onConsole }: { inst: view.Instance; stackKey: string; onConsole?: () => void }) {
  const addr = inst.connect?.find((r) => r.k === "URL" || r.k === "Address")?.v;
  return (
    <>
      <div className="empty" style={{ flex: "none", padding: "36px 40px 20px" }}>
        <h2>{inst.name} is asleep.</h2>
        <p>
          It wakes on the next connection{addr ? <> to <span className="mono">{addr}</span></> : ""}, and sleeps again when nothing holds it. Its data stays in between.
        </p>
        <div className="acts">
          {inst.web ? (
            <button className="btn pri" onClick={() => { void run(stackKey, "wake", inst.name); onConsole?.(); }}>
              Wake and open the console
            </button>
          ) : (
            <button className="btn pri" onClick={() => void run(stackKey, "wake", inst.name)}>
              Wake <kbd>w</kbd>
            </button>
          )}
          {inst.web && (
            <button className="btn" onClick={() => void run(stackKey, "wake", inst.name)}>
              Just wake
            </button>
          )}
        </div>
      </div>
      {(inst.connect ?? []).length > 0 && (
        <div style={{ padding: "0 28px 16px" }}>
          <section className="card">
            <h3>
              Connect <span className="dim" style={{ textTransform: "none", letterSpacing: 0, fontWeight: 400 }}>· works while asleep: the first connection wakes it</span>
            </h3>
            <KVRows rows={inst.connect ?? []} />
          </section>
        </div>
      )}
    </>
  );
}

function TaskOverview({ inst, stackKey, onRuns }: { inst: view.Instance; stackKey: string; onRuns: () => void }) {
  const runs = inst.runs ?? [];
  const needs = inst.needs ?? [];
  if (runs.length > 0) return <LastRun inst={inst} stackKey={stackKey} onRuns={onRuns} />;
  return (
    <div className="two">
      <div className="col">
        <div className="empty" style={{ padding: "28px 20px", flex: "none", alignItems: "flex-start", textAlign: "left" }}>
          <h2>{inst.name} has not run yet.</h2>
          <p>A task runs once when you say so, and then gets out of the way: it keeps nothing awake and nothing waits on it. Run does the steps below, and the output lands on this page as it comes.</p>
        </div>
        <div className="lib" style={{ margin: 0 }} aria-label="What Run does">
          {needs.length > 0 && (
            <div className="lrow" style={{ gridTemplateColumns: "28px 150px minmax(0,1fr)" }}>
              <span className="step-num">1</span>
              <b className="bright">wake {needs.map((n) => n.name).join(", ")}</b>
              <span className="dim ell">{needs.every((n) => n.look.word === "awake") ? "awake now, so nothing to wait for" : "asleep, about a second"}</span>
            </div>
          )}
          <div className="lrow" style={{ gridTemplateColumns: "28px 150px minmax(0,1fr)" }}>
            <span className="step-num">{needs.length > 0 ? 2 : 1}</span>
            <b className="bright">run {inst.run || "the command"}</b>
            <span className="dim ell">with the stack's variables</span>
          </div>
          <div className="lrow" style={{ gridTemplateColumns: "28px 150px minmax(0,1fr)" }}>
            <span className="step-num">{needs.length > 0 ? 3 : 2}</span>
            <b className="bright">report</b>
            <span className="dim ell">exit code, how long it took, the output kept here; a notification only if it fails</span>
          </div>
        </div>
        <p className="model" style={{ margin: 0 }}>
          From a terminal: <span className="mono bright">doze task {inst.name}</span>. From a script: the same, with <span className="mono">--json</span>.
        </p>
      </div>
      <div className="col">
        {(inst.run || (inst.connect ?? []).length > 0 || needs.length > 0) && (
        <section className="card">
          <h3>Command</h3>
          <KVRows rows={(inst.connect ?? []).length ? (inst.connect ?? []) : inst.run ? [{ k: "run", v: inst.run, copy: inst.run, mono: true }] : []} />
          {needs.length > 0 && (
            <div className="kv">
              <span className="k">needs</span>
              <span className="v">{needs.map((n) => n.name).join(", ")}</span>
              <span />
            </div>
          )}
        </section>
        )}
        <section className="card" style={{ flex: 1 }}>
          <h3>Output</h3>
          <div className="code dim" style={{ flex: 1, minHeight: 120, whiteSpace: "normal" }}>
            nothing yet · appears here while it runs, and stays after
          </div>
        </section>
      </div>
    </div>
  );
}

// copyOutput copies a run's lines (the last run when none is named), as kept on the page.
async function copyOutput(stackKey: string, name: string, seq?: number) {
  const t = await transport();
  const l = await t.logs(stackKey, { instance: name, level: 0, from: 0, n: 4000 });
  const lines = l.lines ?? [];
  const which = seq ?? Math.max(0, ...lines.map((x) => x.run ?? 0));
  const text = lines
    .filter((x) => !x.doze && (x.run ?? 0) === which)
    .map((x) => x.line)
    .join("\n");
  await copy(text, "the output");
}

function LastRun({ inst, stackKey }: { inst: view.Instance; stackKey: string; onRuns: () => void }) {
  const runs = inst.runs ?? [];
  // The run whose output is shown: the newest until another is picked.
  const [seq, setSeq] = useState<number | undefined>(undefined);
  const last = runs.find((r) => r.seq === seq) ?? runs[0];
  const longest = Math.max(0.001, ...runs.map((r) => r.secs ?? 0));
  return (
    <div className="two" style={{ gridTemplateColumns: "minmax(0,1.3fr) minmax(0,1fr)" }}>
      <div className="col">
        <section className="card" style={{ flex: 1, minHeight: 0 }}>
          <h3>
            Output
            {last && (
              <span className="dim" style={{ textTransform: "none", letterSpacing: 0, fontWeight: 400 }}>
                · run {last.seq} · {last.when} · {last.done ? `exit ${last.exit}` : "running"} · {last.took}
              </span>
            )}
            <button className="copy" onClick={() => void copyOutput(stackKey, inst.name, last?.log || undefined)}>
              Copy
            </button>
          </h3>
          <div className="dock" style={{ margin: 0, flex: 1 }}>
            <LogsPage stackKey={inst.key} instance={inst.name} embedded bare run={last?.log || undefined} />
          </div>
        </section>
      </div>
      <div className="col">
        <section className="card">
          <h3>
            Runs <span className="dim" style={{ textTransform: "none", letterSpacing: 0, fontWeight: 400 }}>· kept for this stack, newest first</span>
          </h3>
          <div className="runs" role="table" aria-label="Runs">
            <div className="rr h">
              <span>When</span>
              <span>Took</span>
              <span>Exit</span>
              <span>Started by</span>
              <span />
            </div>
            {runs.map((r) => (
              <div key={r.at} className={"rr" + (r.seq === last?.seq ? " sel" : "")}>
                <span className="mono">{r.when}</span>
                <span className="dur">
                  <i style={{ width: Math.max(6, Math.round(((r.secs ?? 0) / longest) * 40)), background: r.done && !r.ok ? "var(--bad)" : undefined }} />
                  {r.took}
                </span>
                <span className={r.done ? (r.ok ? "ok" : "bad") : "cool"}>{r.done ? r.exit : "…"}</span>
                <span className="m" title={r.why}>
                  {r.why}
                </span>
                <button className="copy" aria-pressed={r.seq === last?.seq} onClick={() => setSeq(r.seq)}>
                  {r.seq === last?.seq ? "Shown" : "Output"}
                </button>
              </div>
            ))}
          </div>
        </section>
        {(inst.run || (inst.needs ?? []).length > 0) && (
          <section className="card">
            <h3>Command</h3>
            {inst.run && (
              <div className="kv">
                <span className="k">run</span>
                <span className="v b" title={inst.run}>
                  {inst.run}
                </span>
                <button className="copy" onClick={() => void copy(inst.run ?? "", "the command")}>
                  Copy
                </button>
              </div>
            )}
            {(inst.needs ?? []).length > 0 && (
              <div className="kv">
                <span className="k">needs</span>
                <span className="v">{(inst.needs ?? []).map((n) => n.name).join(", ")}</span>
                <span className="dim" style={{ fontSize: 12 }}>woken first if asleep</span>
              </div>
            )}
          </section>
        )}
        <p className="model" style={{ margin: 0 }}>A task keeps nothing awake once it ends. A failed run shows a notification and the row goes red until the next run.</p>
        <span style={{ display: "none" }}>{stackKey}</span>
      </div>
    </div>
  );
}

// WebConsole is a service's own web console, framed in the tab as the
// ConsoleTab board draws it: a thin line saying what is shown, with the
// way out to the browser, and the console itself, told the app's theme.
// Loading it is a connection, so an asleep service wakes for it; the frame
// says so until the console answers. It is dropped when the tab is left,
// so a console nobody looks at holds nothing.
function WebConsole({ inst }: { inst: view.Instance; stackKey: string }) {
  const dark = useStore((s) => s.dark);
  const [loaded, setLoaded] = useState(false);
  const asleep = inst.look.word === "asleep";
  const url = inst.web ?? "";
  const src = url ? url + (url.includes("?") ? "&" : "?") + "theme=" + (dark ? "dark" : "light") : "";
  useEffect(() => setLoaded(false), [src]);
  return (
    <div className="frame" style={{ marginTop: 16, marginBottom: 0 }}>
      <div className="ctop">
        <span>{inst.name}</span>
        <span className="crumb mono">· {url}</span>
        {!loaded && <span className="dim">· {asleep ? "waking it for the console…" : "loading…"}</span>}
        <span className="grow" />
        {(inst.connect ?? []).find((r) => r.copy) && (
          <button className="btn quiet" onClick={() => void copy((inst.connect ?? []).find((r) => r.copy)?.copy ?? "", "the address")} title={(inst.connect ?? []).find((r) => r.copy)?.v}>
            Copy address <kbd>y</kbd>
          </button>
        )}
        <button className="btn" onClick={() => transport().then((t) => t.open(url))}>
          Open in browser <kbd>o</kbd>
        </button>
      </div>
      {src ? (
        <iframe title={`${inst.name}'s console`} src={src} onLoad={() => setLoaded(true)} style={{ flex: 1, minHeight: 0, width: "100%", border: 0, background: "var(--well)" }} />
      ) : (
        <div className="empty">
          <p className="dim">{inst.name} says it has a console but gave no address.</p>
        </div>
      )}
    </div>
  );
}

// PortClash is the PortClash board: the port an app wants is held by
// something else, named, and the ways out, none of them destructive
// without a click that says exactly what it does.
function PortClash({ inst, stackKey }: { inst: view.Instance; stackKey: string }) {
  const h = inst.crash?.held;
  const dir = useStore((s) => s.stacks.find((x) => x.key === stackKey)?.dir);
  if (!h) return null;
  const who = `${h.command || "a process"}, pid ${h.pid}`;
  return (
    <section className="card clash" aria-label={`Port ${h.port} is taken`}>
      <h3>
        Port {h.port} is taken <span className="dim">· {inst.name} cannot listen while {who} has it</span>
      </h3>
      <div className="opts">
        {!h.doze ? (
          <div className="opt">
            <span className="step-num">1</span>
            <div className="t">
              <b>Stop pid {h.pid} and start {inst.name}</b>
              <span>doze sends it a polite TERM. It is not one of doze's; doze looks again that it still holds the port first.</span>
            </div>
            <button className="btn" onClick={() => transport().then((t) => t.stopHolder(stackKey, inst.name, h.pid)).catch((e) => void say(String(e).replace(/^Error: /, ""), true))}>
              Stop {h.command || "it"}
            </button>
          </div>
        ) : (
          <div className="opt">
            <span className="step-num">1</span>
            <div className="t">
              <b>{h.stack ? `It is ${h.service} in ${h.stack}` : "It is another doze program"}</b>
              <span>Two stacks want the same port. Bring that stack down from Home, or give one of them another port.</span>
            </div>
            <button className="btn" onClick={() => void go({ key: "", page: "library" })}>
              Home <kbd>⌘0</kbd>
            </button>
          </div>
        )}
        <div className="opt">
          <span className="step-num">2</span>
          <div className="t">
            <b>Use another port</b>
            <span>Change port = {h.port} in doze.hcl; {inst.name}'s name keeps working on whatever you pick.</span>
          </div>
          <button className="btn" onClick={() => transport().then((t) => t.open("file://" + (dir ?? "") + "/doze.hcl"))}>
            Edit doze.hcl
          </button>
        </div>
      </div>
      <p className="dim" style={{ margin: 0, fontSize: "var(--text-sm)" }}>
        doze never stops a process it did not start without asking, which is why this is a question and not a thing that happened.
      </p>
    </section>
  );
}

/** STATE_TITLE heads the card that says why a service is in its state, in the state's own word. */
const STATE_TITLE: Record<string, string> = {
  awake: "Why it's awake",
  dozing: "Dozing",
  waking: "Waking",
  asleep: "Asleep",
  off: "Turned off",
  paused: "Paused in doze.hcl",
  failed: "Failed",
};

