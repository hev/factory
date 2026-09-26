import type { APIRoute } from "astro";
import { docsNav, getAllDocs, getDocHref } from "../lib/docs";

const SITE = "https://hevfactory.com";

export const GET: APIRoute = async () => {
	const all = await getAllDocs();
	const byId = new Map(all.map((entry) => [entry.id, entry]));

	const lines: string[] = [];
	lines.push("# hev factory");
	lines.push("");
	lines.push("> Always working: hevbot, a coding agent with its own GitHub account, on a Mac you own.");
	lines.push("");
	lines.push(
		"hev factory runs background coding agents on machines you own, coordinated by the Claude session you already have open on your laptop. Everything it does on the always-on host it does as hevbot, a bot account of its own with its own GitHub login, git author, model subscriptions and vault, so its pull requests are reviewed like a colleague's and nothing it does is ever done as you.",
	);
	lines.push("");
	lines.push(
		"There are four things on the floor. A session is one task in its own worktree or container that ends in a pull request; a goal is a session that sleeps between visits until a check passes. A line is a set of repos worked as one, with an image baked nightly so sessions start warm and isolated. A loop is scheduled work behind a cheap shell gate, so a loop with nothing to do costs nothing. The board is where sessions leave notes for the next session, searched the same way as traces. The dashboard extends hev kit's trace dashboard with all four. Sessions and hosts run today; lines, loops-as-packages and the dashboard are being built, and each page says which parts are real.",
	);
	lines.push("");
	lines.push(`The full concatenated docs are at ${SITE}/llms-full.txt.`);
	lines.push("");

	for (const group of docsNav) {
		lines.push(`## ${group.label}`);
		for (const id of group.items) {
			const entry = byId.get(id);
			if (!entry) continue;
			lines.push(`- [${entry.data.title}](${SITE}${getDocHref(id)}): ${entry.data.description}`);
		}
		lines.push("");
	}

	return new Response(lines.join("\n"), {
		headers: { "Content-Type": "text/plain; charset=utf-8" },
	});
};
