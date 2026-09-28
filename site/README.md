# hevfactory.com

The site for hev factory after the bitter-lesson pivot: coding agents in the
background on a Mac you own. The open build is that one Mac. Pro adds a
laptop client and an always-on server that works as hevbot. It runs ahead of the code on purpose.
Every page opens with a status callout that says what runs today and what is
unbuilt, and those callouts come off one component at a time as things ship.

Twelve pages, in two editions:

| | |
|---|---|
| Start | `index` (Introduction), `quickstart`, `editions` |
| Agent roles | `reception`, `gaffer`, `sessions` |
| Concepts | `identity`, `lines`, `traces`, `loops`, `message-board`, `console` |

Every page renders once per edition, at `/docs/oss/…` and `/docs/pro/…`, with
the Pro | OSS switch from hevlayer.com's docs at the top of the sidebar. The
unprefixed `/docs/…` routes redirect to the edition the reader picked last,
defaulting to OSS. A page only one edition has sets `editions: ["pro"]` in its
front matter, and text that differs within a page goes in
`<Edition only="oss">` or `<Edition only="pro">` (unnested, blank lines inside).

## The rule

**A docs page has to be an agent role (reception, the gaffer, sessions) or a
concept (identity, lines, traces, loops, the message board, the console),
made concrete.
Otherwise it doesn't ship.** An earlier version grew to thirty pages because
nothing stopped the fifteenth, and the role era (picker, foreman, charters)
was deleted rather than deprecated.

The characters are for the marketing page only. The homepage has four, in the
order work passes between them: the operator and hevbot, cut from
`public/art/factory-floor.jpg`, and reception and the gaffer, from the
role-era site (factory-pro, before 05b67f7). The docs name roles and
processes, never characters.

Two more house rules:

- **Shorter is better.** If a page can lose a section, it should.
- **One picture, and it is a screenshot.** The picture is
  `src/components/Console.astro`, a static mock of `factory serve` in kit's
  own visual language, with invented data. Its tabs, columns and routes are the
  spec the real console is built against, so change the mock when the design
  changes, and not only when the copy does.

The docs sit on the dusk sky and skyline from `public/art/parallax/`, fixed
behind the panels.

## Local development

```bash
pnpm install
pnpm dev        # :4341  (mind 4321, layer 4331, factory 4341)
pnpm build      # static build + llms.txt corpus
pnpm deploy     # Cloudflare Pages, project `hevfactory`
```

Stack mirrors `lyr/layer/site` so both properties share one design system:
Astro 5 + MDX on the Cloudflare adapter, hev brand tokens, Inter + JetBrains
Mono, dark-default with a pre-paint theme toggle, docs as a content collection,
`@hevmind/ask` for search and the `llms.txt` corpus, Loops capture with
`userGroup: "hev factory"`.
