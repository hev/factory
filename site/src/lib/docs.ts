import { getCollection, type CollectionEntry } from "astro:content";

export type DocEntry = CollectionEntry<"docs">;

// Two people and four things on the floor, plus the screen that shows them.
// A page has to be one of those made concrete, or it doesn't ship.
export const docsNav = [
	{
		label: "Start",
		items: ["index", "quickstart"],
	},
	{
		label: "The floor",
		items: ["hevbot", "sessions", "lines", "loops", "board", "dashboard"],
	},
] as const;

// Pages that stay in the docs corpus (llms.txt, llms-full.txt, search) but are
// kept out of the rendered sidebar nav — linked from the body or footer instead.
const unlisted = new Set<string>([]);

export function getDocHref(id: string): string {
	return id === "index" ? "/docs" : `/docs/${id}`;
}

export async function getAllDocs(): Promise<DocEntry[]> {
	return await getCollection("docs");
}

export async function getDocNavGroups() {
	const all = await getAllDocs();
	const byId = new Map(all.map((entry) => [entry.id, entry]));
	return docsNav.map((group) => ({
		label: group.label,
		items: group.items
			.filter((id) => !unlisted.has(id))
			.map((id) => byId.get(id))
			.filter((entry): entry is DocEntry => Boolean(entry)),
	}));
}

export async function getDocSiblings(id: string) {
	const all = await getAllDocs();
	const byId = new Map(all.map((entry) => [entry.id, entry]));
	const ordered: string[] = docsNav
		.flatMap((group) => [...group.items])
		.filter((navId) => !unlisted.has(navId));
	const index = ordered.indexOf(id);
	const previous = index > 0 ? byId.get(ordered[index - 1]) : undefined;
	const next =
		index >= 0 && index < ordered.length - 1 ? byId.get(ordered[index + 1]) : undefined;
	return { previous, next };
}

export async function getDocSearchIndex() {
	const all = await getAllDocs();
	return all.map((entry) => ({
		title: entry.data.title,
		description: entry.data.description,
		group: entry.data.group,
		href: getDocHref(entry.id),
		text: [entry.data.title, entry.data.description, entry.data.group].join(" "),
	}));
}
