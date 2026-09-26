# hevfactory.com

The site for hev factory after the bitter-lesson pivot: hevbot, a coding agent
with its own account, on a Mac you own. It runs ahead of the code on purpose.
Every page opens with a status callout that says what runs today and what is
unbuilt, and those callouts come off one component at a time as things ship.

Eight pages:

| | |
|---|---|
| Start | `index`, `quickstart` |
| The floor | `hevbot`, `sessions`, `lines`, `loops`, `board`, `dashboard` |

## The rule

**A page has to be one of the two people (hevbot and the operator), or one of
the four things on the floor (sessions, lines, loops, the board), made
concrete. Otherwise it doesn't ship.** The dashboard earns its page by being
the screen all four show up on. An earlier version grew to thirty pages because
nothing stopped the fifteenth, and the role era (picker, reception, gaffer,
RFCs) was deleted rather than deprecated.

Two characters and no more. hevbot and the operator are cut from
`public/art/factory-floor.jpg`. Everything else gets a screen, not a character.

Two more house rules:

- **Shorter is better.** If a page can lose a section, it should.
- **One picture, and it is a screenshot.** The picture is
  `src/components/Dashboard.astro`, a static mock of `factory serve` in kit's
  own visual language, with invented data. Its tabs, columns and routes are the
  spec the real dashboard is built against, so change the mock when the design
  changes, and not only when the copy does.

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
