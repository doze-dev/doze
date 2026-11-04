import { useEffect, useState } from "react";
import { transport, type serve } from "../transport";
import { go } from "../store";
import { Keys } from "../components/keys";

// Recent messages, as the Notices board draws them: the last twenty things
// said, newest first.
export function RecentPage({ stackKey, stack }: { stackKey: string; stack: string }) {
  const [recent, setRecent] = useState<serve.Notice[]>([]);
  const version = 0;
  useEffect(() => {
    transport().then(async (t) => setRecent((await t.recent()) ?? []));
  }, [version]);
  return (
    <>
      <div className="head">
        <h1>{stack}</h1>
        <span className="grow" />
        <button className="btn" onClick={() => void go({ key: stackKey, page: "home" })}>
          Back <kbd>esc</kbd>
        </button>
      </div>
      <section className="msgs" aria-label="Recent messages">
        <div className="h">
          <b>Recent messages</b>
          <span className="dim">the last 20 · from ⌘K › recent messages</span>
        </div>
        {recent.map((n, i) => (
          <div key={i} className="l">
            <span className="dim mono">{new Date(n.at).toLocaleTimeString([], { hour12: false })}</span>
            <span className={n.bad ? "bad" : ""}>{n.text}</span>
          </div>
        ))}
        {recent.length === 0 && (
          <div className="l">
            <span />
            <span className="dim">
              <i>nothing has been said yet</i>
            </span>
          </div>
        )}
      </section>
      <Keys keys={[["esc", "back"]]} />
    </>
  );
}
