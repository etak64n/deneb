// The documentation site of deneb, built with Starlight. .github/workflows/docs.yml
// publishes it to GitHub Pages at https://etak64n.github.io/deneb/.
import starlight from "@astrojs/starlight";
import { defineConfig } from "astro/config";

export default defineConfig({
  site: "https://etak64n.github.io",
  // GitHub Pages serves the site under the name of the repository.
  base: "/deneb",
  integrations: [
    starlight({
      title: "deneb",
      description: "A self-hosted video API in one Docker image.",
      // The logo stands for the title, in the copy that the color scheme of the page shows well.
      logo: {
        light: "./src/assets/deneb-wordmark.png",
        dark: "./src/assets/deneb-wordmark-dark.png",
        alt: "deneb",
        replacesTitle: true,
      },
      // The icon of the browser tab, as in the dashboard: favicon.svg turns its tile lighter
      // on dark tabs, and favicon.png is for browsers without SVG icons.
      favicon: "/favicon.svg",
      head: [{ tag: "link", attrs: { rel: "icon", type: "image/png", sizes: "64x64", href: "/deneb/favicon.png" } }],
      social: [{ icon: "github", label: "GitHub", href: "https://github.com/etak64n/deneb" }],
      editLink: { baseUrl: "https://github.com/etak64n/deneb/edit/main/docs/" },
      customCss: ["./src/styles/custom.css"],
      // The same groups as the pages of kickd: getting started, guides, settings,
      // reference, internals, and the project itself.
      sidebar: [
        { label: "Getting started", items: ["getting-started/quickstart", "getting-started/deploy"] },
        {
          label: "Guides",
          items: [
            "guides/uploads",
            "guides/playback",
            "guides/webhooks",
            "guides/captions",
            "guides/storage",
            "guides/dashboard",
            "guides/operations",
          ],
        },
        { label: "Settings", items: ["settings/configuration"] },
        {
          label: "Reference",
          items: [
            {
              label: "API",
              items: [
                { label: "Overview", slug: "reference/api" },
                "reference/api/videos",
                "reference/api/uploads",
                "reference/api/playback",
                "reference/api/webhooks",
              ],
            },
            "reference/encoding",
          ],
        },
        { label: "Internals", items: ["internals/architecture"] },
        { label: "Project", items: ["project/limits", "project/development"] },
      ],
    }),
  ],
});
