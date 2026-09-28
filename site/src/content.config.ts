import { defineCollection, z } from "astro:content";
import { glob } from "astro/loaders";

const docs = defineCollection({
	loader: glob({ pattern: "**/*.{md,mdx}", base: "./src/content/docs" }),
	schema: z.object({
		title: z.string(),
		description: z.string(),
		group: z.string(),
		order: z.number(),
		// Which editions a page is in. Omitted means both. Text that differs
		// within a page goes in <Edition only="oss"> or <Edition only="pro">.
		editions: z.array(z.enum(["oss", "pro"])).nonempty().default(["oss", "pro"]),
	}),
});

export const collections = { docs };
