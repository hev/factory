# hev loop: every loop on the mini is a CRD-shaped manifest in git, an object in postgres, and a Run you can list from the laptop

> Source: https://linear.app/hevmind/issue/FAC-3/hev-loop-every-loop-on-the-mini-is-a-crd-shaped-manifest-in-git-an

As the operator of a growing set of loops on the mini (four promo channels today, finance and GTM next), I want each loop declared in one Kubernetes-shaped manifest, scheduled by one service that holds cadence, cooldown and ceiling as data, and every run recorded with its exit and log, so that adding a loop is a manifest and a commit, and suspending one is a single command from the laptop or a phone, not a launchd plist and a prompt edit.

## Why now

Seven launchd timers run loops on the mini today (promo x at 900s, ahev, li, bsky, the factory beat, heartbeat, foreman). Cadence lives in `StartInterval`; cooldown and ceiling ("48h cooldown, 3/week") live in prompt text; memory is one JSONL ledger per script. The promo README documents the consequence: "both loops stop themselves on auth errors, quiet, not loud." The hev/loop repo's May design (retro substrate on DuckDB, MinIO, dbnl) never ran on the mini, and kit RFC 0002 made Layer the archive, which retires that stack. The next loops (finance close over simplefin, the pipeline scan, GTM outreach) would each add another plist and another ledger. The lab may one day be a cluster; a scheduler whose objects are already custom resources ports there with a CRD and a controller, not a rewrite.

## Acceptance criteria

* `loop get loops` on the mini lists every Loop with schedule, suspended, next run, last run, and last exit.
* `loop suspend <name>`, `loop resume <name>`, `loop run <name>` take effect within one reconcile interval (30s).
* The four promo loops run on hev loop with their launchd plists unloaded, and `log/posted-*.jsonl` in hevmind-promo keeps growing at the same cadence over a 48-hour soak.
* Cooldown and ceiling are enforced by the controller, not the prompt: `loop get runs -l loop=ahev` shows Runs with phase `Skipped` and reason `Cooldown`, and never more than 3 executed Runs in any 7 days.
* Every manifest validates against the schema `loop crd` prints; an unknown spec field or a bad cron is rejected by `loop apply` with the field path. Destroying the postgres volume and running `loop apply -f` restores every Loop and its next-run time; Run history is the per-run logs and the ledgers, which survive.

> Remote-host execution is struck under https://linear.app/hevmind/issue/FAC-20. Factory acceptance runs on the mini; operator use from other devices is not a factory gate.

## How to test it

1. `loop apply -f ~/workspace/lab/loops/` on the mini creates Loops x, ahev, li, bsky; `loop get loops` shows four; unload the four plists.
2. After 48 hours, `loop get runs -l loop=x --since 48h` shows roughly 190 Runs, each with a phase, exit code and log path, and the x ledger grew.
3. `loop suspend x` on the mini; no Run in 30 minutes; `loop resume x`; a Run within 15 minutes.
4. `lab down loop`, remove the volume, `lab up loop`, `loop migrate`, `loop apply -f`: four Loops back, next runs correct.
5. `loop run smoke` on a Loop whose command is `true` records a Run with phase `Succeeded` and a log within 30s. `loop crd | kubeconform -strict` passes.

## The shape

```yaml
apiVersion: loop.hevkit.com/v1alpha1
kind: Loop
metadata:
  name: promo-ahev
  labels: {channel: x}
spec:
  schedule: "0 */6 * * *"
  concurrencyPolicy: Forbid
  cooldown: 48h
  ceiling: {count: 3, window: 168h}
  template:
    workingDir: ~/workspace/hevmind-promo
    command: ["zsh", "-l", "scripts/run-iteration-ahev.sh"]
```

A `Run` is to a `Loop` what a Job is to a CronJob: created by the controller, owned by its Loop, `spec` a snapshot of the template, `status` carrying phase, exit code, timestamps, log path and session id.

## The work

1. **Reset the repo.** Tag current `main` as `retro-v0`; move `docs/` to `docs/archive/retro/`; delete docker-compose.yml, grafana, nginx, the dbnl proxy, `cmd/hev`. New README states what loop is in three sentences. *Accept:* `git tag` lists retro-v0 and `go build ./...` passes on the new tree.
2. **The object store.** One table, `objects(uid, api_version, kind, name, resource_version, labels jsonb, owner_uid, spec jsonb, status jsonb, created_at, updated_at, deleted_at)`, unique on (kind, name), optimistic concurrency on `resource_version`, an expression index on `status` for the controller's due query. Kinds are `Loop` and `Run`. pgx, embedded SQL migrations, `loop migrate` idempotent. *Accept:* test 4.
3. **The manifest.** One YAML per loop in `lab/loops/<name>.yaml`, a custom resource in group `loop.hevkit.com`. `spec` mirrors CronJob wherever CronJob has the field (`schedule`, `suspend`, `concurrencyPolicy`, `startingDeadlineSeconds`) and adds `cooldown`, `ceiling: {count, window}`, and a host-shaped `template: {workingDir, command, env}`. Secrets stay where they are: the command runs under `zsh -l` and each script's `load-secrets.sh` resolves from 1Password as today. `loop apply -f <dir> --prune` upserts by kind and name and suspends Loops whose file is gone. *Accept:* tests 1 and 4.
4. **The schema.** `schema/loop.hevkit.com_v1alpha1.json` is the OpenAPI v3 schema a CRD carries, embedded and enforced by `loop apply`; `loop crd` prints a `CustomResourceDefinition` generated from it. *Accept:* criterion 5 and test 5.
5. **API and CLI, one binary.** `loop serve` is HTTP and JSON on `:7710` bound to the Tailscale address (kit serve is 8787, board is 7700), with paths in the API-server shape: `/apis/loop.hevkit.com/v1alpha1/loops/<name>` and `.../runs`, GET, PUT with `resourceVersion`, PATCH. The CLI is kubectl-shaped: `loop apply -f`, `loop get loops|runs`, `loop describe loop/<name>`, `loop logs run/<uid>`, `loop suspend|resume <name>` (a patch on `spec.suspend`), `loop run <name>` (creates a Run with reason `Manual`), `loop crd`, `loop migrate`, all via `LOOP_URL`. *Accept:* criteria 1 and 2.
6. **The controller, on the host.** `loop d` under launchd `com.hev.loop` (KeepAlive, declared in hev/lab) reconciles every 30s: for each unsuspended Loop that is due it evaluates cooldown and ceiling against that Loop's Runs and creates a Run (owned by the Loop, `spec` a snapshot of the template, `status.phase` Pending, or Skipped with reason `Cooldown` or `Ceiling`), claimed with `FOR UPDATE SKIP LOCKED`. It executes each Run as `zsh -l -c <command>` in tmux session `loop-<name>`, one at a time per Loop (`concurrencyPolicy: Forbid`), stdout and stderr to `~/.hev/loop/runs/<name>/<uid>.log`, then writes `status` on the Run (phase, exitCode, startedAt, finishedAt, logPath) and on the Loop (`lastScheduleTime`, `lastRun`). When the Run spawned a `claude` session, the newest transcript under `~/.claude/projects` for that workingDir created during the Run is recorded as `status.sessionID`, so `hev trace <id>` opens it. *Accept:* tests 2 and 5.
7. **Migrate promo.** Four manifests in `lab/loops`; the four plists unloaded and removed from `lab/launchd`; ahev's cooldown and ceiling move from `prompt-ahev.md` into its manifest while the prompt keeps the editorial bar; the promo README's Operations table says `loop suspend` instead of `launchctl unload`. *Accept:* tests 2 and 3, and the promo README diff.
8. **Health.** `lab doctor` is red when `loop d` is not running or any unsuspended Loop's `lastScheduleTime` is older than twice its schedule interval. *Accept:* `lab doctor` green after step 7.
9. **The factory seam, stubbed.** `factory-pro/runtimes/loop.sh` applies a Loop manifest for an instance's beat instead of a launchd timer, per `contracts/extending.md` §1, with a README paragraph. Nothing in hev/factory changes. *Accept:* the stub exists and the public build's tests pass unchanged.

## Constraints

* CRD-shaped, no Kubernetes: one node, and Runs must execute on the macOS host where `claude`, `xurl`, `op` and the keychain live. The manifests are valid custom resources for group `loop.hevkit.com`; postgres is a generic object store with `resourceVersion`; the day the lab is a cluster, the port is the CRD that `loop crd` already prints plus a controller, not a rewrite. The manifest in git is the declarative record, postgres is controller state, and logs and ledgers are the history. Losing the database loses nothing.
* Field names follow CronJob and Job wherever those have the field, so nothing is renamed on the port. `status` is written only by the controller; `loop apply` never touches it.
* hev/loop is public (the dev kit), Apache-2.0 like factory. Loop definitions, prompts, and secrets references live in hev/lab and never in hev/loop.
* The binary is `loop`. `hev loop` is dispatch from kit (the tap RFC), not a subcommand built here.
* loop calls no model. It runs commands and records what happened.
* The factory beat stays on launchd in the public build. heartbeat and foreman are not migrated in this RFC.
* No Slack from loop. Iterations keep their own summaries.

## Out of scope

A web page (`loop get` is the read side; a loops tab in kit serve is a later RFC). A span per run into kit's trace model (needs a loop concept in kit RFC 0003; a later kit RFC). Multi-host. The retro math. A cluster, a controller on client-go, watch semantics, or admission webhooks: polling and a schema check are enough for one node.

## Estimate

Object store, schema, API and controller are roughly 2,000 lines of Go. Four to six worker sessions, then the 48-hour soak before the plists are deleted.

## Integration handoff — 2026-09-09

Source steps 1–6 and the step 9 suspended stub are merged; steps 7–8 have suspended source in https://github.com/hev/lab/pull/7 and https://github.com/hev/hevmind-promo/pull/35, both merged. Live migration, health and the soak remain unverified.

- Steps 7–8 next run the reviewed [cutover checklist](https://github.com/hev/lab/blob/main/docs/loop-integration.md#production-cutover-checklist--not-executed), after the operator supplies its activation window, existing credential reference and API access decision at https://linear.app/hevmind/issue/FAC-3. Keep legacy scheduling and suspended source intact until coordinated cutover; preserve rolling admission history.
- Record 48-hour soak, health and schedule-control results against the original acceptance before archive. Database-loss/reapply is an explicit operator-authorized drill, never an implicit source test.
- Step 9 is a stub only; the public runtime still rejects runtime=loop. No factory, heartbeat or foreman timer is migrated.
