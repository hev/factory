# Browser verification setup

The extension seams are in [the extension contract](../contracts/extending.md).
For an instance with `preview_domains`, install the required browser tool on
`home_host` by hand:

```sh
brew install agent-browser
agent-browser install
agent-browser doctor --offline --quick --json
```

`npm install -g agent-browser` is an alternative binary install. ffmpeg is
optional for recording; consult `agent-browser skills get core` for the
installed version's commands. No provisioning or browser MCP server is needed.

Add `preview_domains` to `factories/<instance>.toml`, a list of domain glob
patterns covering public preview deployments and production baselines (see
[example.toml](../factories/example.toml)). Absent means browser verification
is disabled; empty means no permitted destinations. Previews requiring login
are a `[human step]`.

`factory list` shows the configured field. `scripts/factory-health.sh` checks
`agent-browser doctor --offline --quick --json` on each configured instance's
home host and returns nonzero for missing tools, failed doctor commands or
invalid/unsuccessful reports. Optional recording warnings alone are not fatal.

The [loop contract](../contracts/factory-loop.md) defines worker screenshots,
the gaffer's independent check, the ten-minute no-preview fallback, isolated
sessions and cleanup. Evidence stays on the host under `~/.factory/evidence/`
until the corresponding plan's harvest logs are swept; nothing commits it.
