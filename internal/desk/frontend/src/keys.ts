// The page's own keys (space, ⏎, letters) act on what the page has
// selected. They must not fire when the person is on something else: typing
// in a field, on a button or switch they tabbed to (where space and ⏎ are
// that control's own), inside a console's frame, or with a dialog, sheet or
// menu open (which own the keyboard). Tab is never a page key.

/** pageKey says a keydown is for the page. */
export function pageKey(e: KeyboardEvent): boolean {
  if (e.metaKey || e.ctrlKey || e.altKey || e.key === "Tab") return false;
  if (e.defaultPrevented) return false;
  const t = e.target as HTMLElement | null;
  if (t && t !== document.body && t !== document.documentElement) {
    const tag = t.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || tag === "IFRAME" || t.isContentEditable) return false;
    // A focused control keeps space and ⏎; letters still reach the page.
    if ((e.key === " " || e.key === "Enter") && t.closest("button, a, [role=switch], [role=checkbox], [role=radio], [role=tab], [role=menuitem], summary")) return false;
  }
  // Something on top owns the keyboard.
  if (document.querySelector(".scrim, .pop[role=menu]")) return false;
  return true;
}
