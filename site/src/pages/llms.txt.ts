import type { APIRoute } from "astro";
import { canonicalDocHref, docsNav, getAllDocs } from "../lib/docs";

const SITE = "https://hevfactory.com";

export const GET: APIRoute = async () => {
	const all = await getAllDocs();
	const byId = new Map(all.map((entry) => [entry.id, entry]));

	const lines: string[] = [];
	lines.push("# hev factory");
	lines.push("");
	lines.push("> Background coding agents on a Mac you own, coordinated by the Claude session you already have open.");
	lines.push("");
	lines.push(
		"hev factory hands long, parallel or overnight work from the Claude session you already have open to background sessions on a Mac you own. Three agent roles do the work: reception (your Claude session with the factory's skill) files jobs; a gaffer, one per job, runs the job's sessions in order, woken by a model-free tick only when something changed; and each session is one task in its own checkout that ends in a pull request. A session acts as whoever its host is logged in as, never as who asked for it.",
	);
	lines.push("");
	lines.push(
		"There are two editions. The open source build (OSS) runs everything on one Mac, as whoever is logged in there. Pro splits it into a client and a server: reception on your laptop as you, and jobs, gaffers and sessions on an always-on Mac as a bot account of its own (ours is hevbot), plus more than one machine, Linear intake, Slack alerts, the board and lines. Docs pages live at /docs/oss/... and /docs/pro/...; pages marked Pro below are only in pro. Sessions, jobs and the tick run today; the gaffer is being built, and each page says which parts are real.",
	);
	lines.push("");
	lines.push(`The full concatenated docs are at ${SITE}/llms-full.txt.`);
	lines.push("");

	for (const group of docsNav) {
		lines.push(`## ${group.label}`);
		for (const id of group.items) {
			const entry = byId.get(id);
			if (!entry) continue;
			const pro = !entry.data.editions.includes("oss") ? " (Pro)" : "";
			lines.push(`- [${entry.data.title}](${SITE}${canonicalDocHref(entry)})${pro}: ${entry.data.description}`);
		}
		lines.push("");
	}

	return new Response(lines.join("\n"), {
		headers: { "Content-Type": "text/plain; charset=utf-8" },
	});
};
