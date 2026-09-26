import { defineConfig } from "astro/config";
import mdx from "@astrojs/mdx";
import cloudflare from "@astrojs/cloudflare";
import hevAsk from "@hevmind/ask";

export default defineConfig({
	site: "https://hevfactory.com",
	devToolbar: { enabled: false },
	// Pinned lane: mind = 4321, layer = 4331, factory = 4341. strictPort so it
	// fails loudly instead of silently hopping into a neighbouring project's port.
	server: { port: 4341, strictPort: true },
	// Static-first: every page prerenders; only the on-demand /api/ask route
	// runs as a Cloudflare Pages Function.
	adapter: cloudflare({ platformProxy: { enabled: true } }),
	integrations: [
		mdx(),
		hevAsk({ collections: ["docs"], basePath: "/docs/", model: "claude-sonnet-4-6" }),
	],
	markdown: {
		// Shiki over the dark code surface; vesper's peach accent sits close to
		// the site's --signal orange. Background is overridden in DocsLayout to
		// match the existing #0b0b0b code blocks.
		syntaxHighlight: "shiki",
		shikiConfig: { theme: "vesper" },
	},
});
