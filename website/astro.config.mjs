// The doze documentation site. Served at doze.nerdmenot.in (the registry stays
// at /registry, proxied by a Pages Function in the registry project — see
// docs/design/website.md in the repo root).
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";

export default defineConfig({
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
      sidebar: [
        {
          label: "Why doze",
          items: [
            { slug: "why/doze" },
            { slug: "why/not-containers" },
            { slug: "why/hcl" },
            { slug: "why/alternatives" },
            { slug: "why/trust" },
          ],
        },
        {
          label: "Getting started",
          items: [
            { slug: "start/install" },
            { slug: "start/getting-started" },
            { slug: "start/concepts" },
            { slug: "start/from-docker-compose" },
          ],
        },
        {
          label: "The CLI & dashboard",
          items: [
            { slug: "cli/tour" },
            { slug: "cli/dashboard" },
          ],
        },
        {
          label: "Guides",
          items: [
            { slug: "guides/microservices" },
            { slug: "guides/engines" },
            {
              label: "Engine recipes",
              collapsed: true,
              items: [
                { slug: "guides/recipes/postgres" },
                { slug: "guides/recipes/valkey-kvrocks" },
                { slug: "guides/recipes/documentdb" },
                { slug: "guides/recipes/s3" },
                { slug: "guides/recipes/sqs" },
                { slug: "guides/recipes/sns" },
                { slug: "guides/recipes/stacks" },
                { slug: "guides/recipes/config-layout" },
              ],
            },
            { slug: "guides/workflows" },
            { slug: "guides/modules" },
            { slug: "guides/teams-ci" },
            { slug: "guides/files-and-storage" },
            { slug: "guides/resource-footprint" },
            { slug: "guides/troubleshooting" },
            { slug: "guides/faq" },
          ],
        },
        {
          label: "Building modules",
          items: [
            { slug: "modules/overview" },
            { slug: "modules/first-module" },
            { slug: "modules/real-engines" },
            { slug: "modules/describe" },
            { slug: "modules/testing" },
            { slug: "modules/releasing" },
            { slug: "modules/publishing" },
          ],
        },
        {
          // Not under /registry/* — that path is the registry portal itself
          // (proxied by the domain router), so the operator guide lives at
          // /operate/* to avoid the collision.
          label: "Running a registry",
          items: [
            { slug: "operate/trust-architecture" },
            { slug: "operate/self-host" },
            { slug: "operate/mirror-binaries" },
            { slug: "operate/operations" },
            { slug: "operate/roadmap-hosts" },
          ],
        },
        {
          label: "Reference",
          items: [
            { slug: "reference/cli" },
            { slug: "reference/configuration" },
            { slug: "reference/lockfile" },
            { slug: "reference/environment" },
            { slug: "reference/module-index" },
            { slug: "reference/binaries" },
            { slug: "reference/extensions" },
            { slug: "reference/architecture" },
          ],
        },
      ],
    }),
  ],
});
