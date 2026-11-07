// The doze documentation site. Served at doze.nerdmenot.in (the registry stays
// at /registry, proxied by a Pages Function in the registry project — see
// docs/design/website.md in the repo root).
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import { fileURLToPath } from "node:url";

export default defineConfig({
  // Pages written for doze-website import its components as @docs/…; here the
  // alias points at stand-ins so this older site still builds.
  vite: { resolve: { alias: { "@docs": fileURLToPath(new URL("./src/components/docs-standins", import.meta.url)) } } },
  site: "https://doze.nerdmenot.in",
  integrations: [
    starlight({
      title: "doze",
      description:
        "docker-compose for local development, without the virtualization. Real engines as native processes — your machine stays cool, your debugger just attaches.",
      logo: { src: "./src/assets/logo.png", alt: "doze" },
      favicon: "/favicon-32.png",
      // Code blocks read as terminals — one dark theme in BOTH light and dark
      // site modes, so syntax is always high-contrast (a light syntax theme on
      // our warm-paper light mode washed the text out to near-invisible).
      expressiveCode: {
        themes: ["github-dark"],
        styleOverrides: { borderRadius: "0.5rem", borderColor: "transparent" },
      },
      components: {
        // Share the registry portal's nav links, and swap the theme dropdown
        // for a three-segment System/Light/Dark toggle.
        SocialIcons: "./src/components/HeaderNav.astro",
        ThemeSelect: "./src/components/ThemeSelect.astro",
      },
      customCss: ["./src/styles/theme.css"],
      social: [
        { icon: "github", label: "GitHub", href: "https://github.com/doze-dev/doze" },
      ],
      editLink: {
        baseUrl: "https://github.com/doze-dev/doze/edit/main/website/",
      },
      lastUpdated: true,
      head: [
        {
          tag: "link",
          attrs: { rel: "preconnect", href: "https://fonts.googleapis.com" },
        },
        {
          tag: "link",
          attrs: {
            rel: "preconnect",
            href: "https://fonts.gstatic.com",
            crossorigin: true,
          },
        },
        {
          tag: "link",
          attrs: {
            rel: "stylesheet",
            href: "https://fonts.googleapis.com/css2?family=Instrument+Serif&family=Hanken+Grotesk:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;600&display=swap",
          },
        },
      ],
      // The docs are written for doze-website, which orders them by hand
      // (design/docs-ia.json there). This older site lists each folder.
      sidebar: [
        { label: "Start here", autogenerate: { directory: "start" } },
        { label: "The app", autogenerate: { directory: "app" } },
        { label: "doze.hcl", autogenerate: { directory: "hcl" } },
        { label: "Services", autogenerate: { directory: "services" } },
        { label: "Guides", autogenerate: { directory: "guides" } },
        { label: "Reference", autogenerate: { directory: "reference" } },
        { label: "Contribute to the registry", autogenerate: { directory: "contribute" } },
        { label: "Why doze", autogenerate: { directory: "why" } },
      ],
    }),
  ],
});
