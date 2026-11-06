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
      logo: { src: "./src/assets/logo.svg" },
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
        { label: "Reference", items: ["reference/api", "reference/encoding"] },
        { label: "Internals", items: ["internals/architecture"] },
        { label: "Project", items: ["project/limits", "project/development"] },
      ],
    }),
  ],
});
