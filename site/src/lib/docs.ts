import { getCollection, type CollectionEntry } from "astro:content";

export type DocEntry = CollectionEntry<"docs">;
export type Edition = "oss" | "pro";

export const EDITIONS: Edition[] = ["oss", "pro"];
export const DEFAULT_EDITION: Edition = "oss";
export const EDITION_LABELS: Record<Edition, { short: string; long: string }> = {
	oss: { short: "OSS", long: "Open source, one Mac" },
	pro: { short: "Pro", long: "Laptop and server" },
};

// A page has to be an agent role or a concept made concrete, or it doesn't
// ship. Start holds the way in and the line between the editions.
export const docsNav = [
	{
		label: "Start",
		items: ["index", "quickstart", "editions"],
	},
	{
		label: "Agent roles",
		items: ["reception", "gaffer", "sessions"],
	},
	{
		label: "Concepts",
		items: ["identity", "lines", "traces", "loops", "message-board", "console"],
	},
] as const;

// Pages that stay in the docs corpus (llms.txt, llms-full.txt, search) but are
// kept out of the rendered sidebar nav — linked from the body or footer instead.
const unlisted = new Set<string>([]);

export function inEdition(entry: DocEntry, edition: Edition): boolean {
	return entry.data.editions.includes(edition);
}

// With an edition, the page's own view in it; without, the unprefixed route
// that sends a reader to the edition they last picked.
export function getDocHref(id: string, edition?: Edition): string {
	const root = edition ? `/docs/${edition}` : "/docs";
	return id === "index" ? root : `${root}/${id}`;
}

// Points a /docs link written in a page body at the same edition as the page.
export function editionHref(href: string, edition: Edition): string {
	const match = href.match(/^\/docs(?:\/([^#?]*?))?\/?([#?].*)?$/);
	if (!match) return href;
	const [, slug = "", rest = ""] = match;
	if (/^(oss|pro)(\/|$)/.test(slug)) return href;
	return getDocHref(slug || "index", edition) + rest;
}

// A page's canonical link for readers with no edition picked: its OSS view if
// it has one, else its pro view.
export function canonicalDocHref(entry: DocEntry): string {
	return getDocHref(entry.id, inEdition(entry, DEFAULT_EDITION) ? DEFAULT_EDITION : entry.data.editions[0]);
}

// A page body as plain text for llms-full.txt: each <Edition> block kept, and
// labelled with the edition it's true of.
export function plainDocBody(body: string): string {
	return body
		.replace(/^import .*\n/gm, "")
		.replace(/<Edition only="(oss|pro)">\s*([\s\S]*?)\s*<\/Edition>/g, (_, only, inner) =>
			`[${EDITION_LABELS[only as Edition].short} only] ${inner.trim()}`,
		)
		.replace(/\n{3,}/g, "\n\n")
		.trim();
}

export async function getAllDocs(): Promise<DocEntry[]> {
	return await getCollection("docs");
}

export async function getDocNavGroups(edition: Edition) {
	const all = await getAllDocs();
	const byId = new Map(all.map((entry) => [entry.id, entry]));
	return docsNav
		.map((group) => ({
			label: group.label,
			items: group.items
				.filter((id) => !unlisted.has(id))
				.map((id) => byId.get(id))
				.filter((entry): entry is DocEntry => Boolean(entry) && inEdition(entry!, edition)),
		}))
		.filter((group) => group.items.length);
}

export async function getDocSiblings(id: string, edition: Edition) {
	const all = await getAllDocs();
	const byId = new Map(all.map((entry) => [entry.id, entry]));
	const ordered: string[] = docsNav
		.flatMap((group) => [...group.items])
		.filter((navId) => !unlisted.has(navId) && byId.has(navId) && inEdition(byId.get(navId)!, edition));
	const index = ordered.indexOf(id);
	const previous = index > 0 ? byId.get(ordered[index - 1]) : undefined;
	const next =
		index >= 0 && index < ordered.length - 1 ? byId.get(ordered[index + 1]) : undefined;
	return { previous, next };
}

export async function getDocSearchIndex(edition: Edition = DEFAULT_EDITION) {
	const all = (await getAllDocs()).filter((entry) => inEdition(entry, edition));
	return all.map((entry) => ({
		title: entry.data.title,
		description: entry.data.description,
		group: entry.data.group,
		href: getDocHref(entry.id, edition),
		text: [entry.data.title, entry.data.description, entry.data.group].join(" "),
	}));
}
