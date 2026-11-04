import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./app";
import "./tokens.css";
import "./app.css";

// The page reports its first paint so the desk bench can hold the
// window→home budget; Go injects `window.__doze` before the script runs.
const began = performance.now();
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
requestAnimationFrame(() => {
  const painted = Math.round(performance.now() - began);
  document.documentElement.dataset.painted = String(painted);
});
