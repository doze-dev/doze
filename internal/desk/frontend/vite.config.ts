import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The app is served by Go from the built dist/ (embedded in the binary) and,
// in the browser, by `doze open --serve`. The dev server proxies the latter so
// the page hot-reloads against real views.
export default defineConfig({
  plugins: [react()],
  base: "./",
  build: {
    outDir: "dist",
    emptyOutDir: false,
    sourcemap: false,
    target: "safari17",
    // Everything the first paint needs is one chunk; heavy pieces (a map,
    // a terminal) are loaded lazily by their pages when they come.
    chunkSizeWarningLimit: 300,
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:7707",
      "/events": "http://127.0.0.1:7707",
    },
  },
});
