# Every user-visible change arrives with a picture of the deployed preview, taken by the factory in its own headless browser

> Source: https://linear.app/hevmind/issue/FAC-14/every-user-visible-change-arrives-with-a-picture-of-the-deployed

As the operator approving from a phone, I want every user-visible change to reach me as a screenshot the factory took against the deployed preview, with the gaffer's "factory verified" resting on its own browser check rather than the worker's word, so that tapping approve is a decision about a picture I have seen and not a pull request I have to open.

## Why now

The Linear skill already says "screenshot every user-visible change, against the deployed preview, not a local dev server" and "lead with the preview link", and `factory-loop.md` step 6 lists a pull request only after the gaffer "checked its acceptance evidence". No contract names a tool that can do either. A worker has no browser, so for a web change the acceptance check is a diff read and the screenshot rule is honoured by nobody. Both live factories (charlie, lyr) ship websites on Cloudflare Pages with a preview per pull request, so the surface to look at already exists on every change.

agent-browser v0.37.0 (2026-09-08, https://github.com/vercel-labs/agent-browser, via https://x.com/ctatedev/status/2097134173181870550) is one Rust binary that drives headless Chrome from Chrome for Testing: `snapshot` gives an accessibility tree with refs an agent can click, `screenshot` and `diff screenshot --baseline` give the picture and the before-and-after, `record --fps` gives a video of a flow, and `--allowed-domains`, `--content-boundaries` and `--max-output` fence what a worker can reach and how much page text lands in its context. `agent-browser skills get core` prints instructions that match the installed version, and the install works for Claude Code and Codex, which matters because both live factories dispatch Codex workers. It is Apache-2.0 and installs with one brew or npm line, so CE stays hand-satisfiable.

## Acceptance criteria

* Every verified-ready comment on a Linear issue whose change has a user-visible surface opens with the preview link and carries an image the gaffer took against that preview during its own verification, not one the worker supplied.
* A modification carries before and after: production and preview side by side, or a `diff screenshot` image.
* A worker on a step with a user-visible surface cannot reach "factory verified" without `~/.factory/evidence/<instance>/<session>/` holding at least one screenshot named for the acceptance criterion it shows; a pull request without it bounces under the existing evidence-bounce rule.
* A worker's browser refuses to leave the instance's `preview_domains`; navigating anywhere else fails and the failure is in the harvest log.
* Two workers on one instance run in two browser sessions; `agent-browser session list` on the host shows both, named for the tmux session.
* `scripts/factory-health.sh` reports the host red when any instance sets `preview_domains` and `agent-browser doctor --quick --json` is not clean.
* A change with no preview after 10 minutes is reported with one line naming what stands in for the picture, per the Linear skill, never a local screenshot presented as the preview.

## How to test it

1. On the mini, `brew install agent-browser && agent-browser install`, then `agent-browser doctor --json` reports clean. Unlink the binary and run `scripts/factory-health.sh charlie`: red, naming agent-browser.
2. Add `preview_domains = ["*.travelswithcharlie.pages.dev", "travelswithcharlie.com"]` to `factories/charlie.toml`. Approve a one-line copy change on the home page. When the worker's pull request opens, `ls ~/.factory/evidence/charlie/worker-charlie-*/` lists a PNG. When the issue moves to In Review, its comment leads with the preview URL and shows a before and after image.
3. In the worker's session, `agent-browser open example.com` exits non-zero naming the allowlist.
4. Dispatch two UI steps at once; `agent-browser session list` shows two sessions.
5. Approve a change that fails its deploy. The verified-ready comment has one line naming the evidence that stands in, and no image.

## The work

1. **The tool is a declared requirement.** `docs/extending.md` and `README.md` name `agent-browser` plus `agent-browser install` as required on `home_host` for any instance with `preview_domains`, and ffmpeg as optional for recording. `factory-health.sh` runs `agent-browser doctor --offline --quick --json` when the config asks for it. *Accept:* test 1.
2. **Config.** `factories/<instance>.toml` gains `preview_domains`, a list of glob patterns, documented in `factories/example.toml`. Absent, nothing in this RFC applies to that instance. *Accept:* `factory list` shows the field and test 2's config parses.
3. **The worker's browser.** A fifth standing instruction in the brief (factory-loop.md step 3): a step with a user-visible surface opens the pull request's preview URL, read from the deployment status on the pull request, in `agent-browser --session <tmux session> --allowed-domains <preview_domains> --content-boundaries --max-output 20000`, and saves one screenshot per acceptance criterion to the evidence directory under a name that says which one, plus a `record` for any flow longer than one screen. The brief spells out `agent-browser skills get core` as the way to learn the CLI, since Codex has no skill stub. Self-review (b) lists the evidence paths in the pull request body. *Accept:* criterion 3 and test 2.
4. **The gaffer's own look.** Step 6 gains a browser pass before "factory verified": open the preview in the gaffer's own session, snapshot, screenshot the state named in each acceptance criterion, and for a modification `diff screenshot --baseline` against production. Upload with `prepare_attachment_upload` and embed in the verified-ready comment, preview link first. A pull request whose evidence directory is empty on a UI step bounces. *Accept:* criteria 1 and 2 and test 2.
5. **Sessions and cleanup.** `factory-reap.sh` closes the reaped worker's browser session with `agent-browser --session <name> close`, and `--idle-timeout 30m` is on every launch so a dead worker never leaves Chrome running. Evidence directories follow the harvest log's retention. *Accept:* criterion 5 and test 4, and `agent-browser session list` is empty an hour after the floor clears.
6. **No preview, say so.** The brief and step 6 carry the fallback: after 10 minutes with no deployment status, the worker says so with `factory-say.sh note`, the pull request body says what stands in (the test run, the build log), and the comment follows the Linear skill's "no preview" line. *Accept:* criterion 7 and test 5.
7. **The rubric.** `evals/rubric.md` grades a UI step without preview evidence as poor, so the foreman's poor-eval suggestion catches a worker that skipped the browser. *Accept:* one rubric line, and a session from test 5 grades poor only when the stand-in line is also missing.

## Constraints

* CLI only inside the loop. No MCP server in worker or gaffer sessions: the CLI keeps context small, and `--json` is the machine surface. The MCP profile stays available to a person at the desk.
* Page content is untrusted. `--content-boundaries` and `--allowed-domains` are not optional flags in the brief, and a worker never passes `--remote-debugging-port`, `--auto-connect`, or `--profile`, which would put the operator's own logins in a worker's hands.
* Screenshots are taken on `home_host`, never on the laptop, and land in `~/.factory/evidence/`, which no clone shares and nothing commits.
* A step with no user-visible surface is unchanged. A migration or a build fix gets its one line of stand-in evidence as today.
* Previews must be reachable without login. A Cloudflare Access gate makes the step a `[human step]`; the auth vault is out of scope.
* This build never learns the overlay exists: nothing here reads `identity/` or posts outward. The gaffer's Linear upload is the same call the skill already documents.

## Out of scope

WebMCP, chat mode, the iOS simulator, hosted browser providers. Gating on `a11y` or `vitals` (report a count in the comment if it is free; never block on it). Vercel-protected deployments. The phone widget that would show these pictures.

## Estimate

Roughly 200 lines of shell and contract text, no Go. Two worker sessions on this repo, then one approved copy change on charlie for tests 2 through 5.

## Integration and acceptance follow-through — 2026-09-08

All seven source items merged in https://github.com/hev/factory/pull/13. https://github.com/hev/factory/pull/11 also merged, despite the recorded overlap in standing-instruction letters and count.

8. **Reconcile the merged contract.** Verify the combined instructions preserve CI backoff, stopping at the PR handoff, worker preview evidence, and independent gaffer browser review. Correct duplicate letters/counts without changing their policies in one operator-gated `[contract]` PR. Acceptance: one unambiguous list and its references, full diff review, Go and preview-browser fixture checks pass.
9. **Verify live browser acceptance.** Install/check the browser on the mini and run non-disruptive local isolation, allowlist and cleanup checks. Configured preview domains and the next already-approved UI change provide the evidence for live tests 2–5; do not invent a sibling-factory task or alter its config. Keep the plan active until these checks have evidence; report missing preview configuration as a remaining prerequisite, not a claimed pass.
