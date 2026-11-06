import { useEffect, type RefObject } from "react";

const focusable = 'button:not(:disabled), [href], input:not(:disabled), select, textarea, [tabindex]:not([tabindex="-1"])';

/**
 * useTrap makes an open dialog modal for the keyboard: focus moves into it
 * (to the element marked data-autofocus, else the first control), Tab and
 * shift-Tab cycle inside it, and when it closes focus returns to where it
 * was, so a keyboard user is never left behind the scrim.
 */
export function useTrap(ref: RefObject<HTMLElement | null>) {
  useEffect(() => {
    const box = ref.current;
    if (!box) return;
    const before = document.activeElement as HTMLElement | null;
    const all = () => Array.from(box.querySelectorAll<HTMLElement>(focusable)).filter((el) => el.offsetParent !== null);
    if (!box.contains(document.activeElement)) (box.querySelector<HTMLElement>("[data-autofocus]") ?? all()[0] ?? box).focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Tab") return;
      const els = all();
      if (els.length === 0) return;
      const i = els.indexOf(document.activeElement as HTMLElement);
      e.preventDefault();
      els[(i + (e.shiftKey ? els.length - 1 : 1) + els.length) % els.length].focus();
    };
    box.addEventListener("keydown", onKey);
    return () => {
      box.removeEventListener("keydown", onKey);
      if (before && document.contains(before)) before.focus();
    };
  }, [ref]);
}
