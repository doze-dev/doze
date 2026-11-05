import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./tokens.css";
import "./shell.css";
import { App } from "./app";

// The page reports its first paint of home to Go (app.tsx), for the desk
// bench's window→home budget.
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
