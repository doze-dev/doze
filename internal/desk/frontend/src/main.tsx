import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./app";
import "./tokens.css";
import "./app.css";

// The page reports its first paint of home to Go (app.tsx), for the desk
// bench's window→home budget.
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
