import type { APIRoute } from "astro";
import { docsNav, getAllDocs, getDocHref } from "../lib/docs";

const SITE = "https://hevfactory.com";

export const GET: APIRoute = async () => {
	const all = await getAllDocs();
	const byId = new Map(all.map((entry) => [entry.id, entry]));

	const parts: string[] = [];
	parts.push("# hev factory — full docs\n\n");
	parts.push(`> Every docs page, concatenated. Index at ${SITE}/llms.txt.\n\n`);

	for (const group of docsNav) {
		for (const id of group.items) {
			const entry = byId.get(id);
			if (!entry) continue;
			parts.push(`---\n\n# ${entry.data.title}\n\n`);
			parts.push(`Source: ${SITE}${getDocHref(id)}\n\n`);
			parts.push(`${(entry.body ?? "").trim()}\n\n`);
		}
	}

	return new Response(parts.join(""), {
		headers: { "Content-Type": "text/plain; charset=utf-8" },
	});
};
