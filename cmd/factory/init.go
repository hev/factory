// factory init — write factories/<name>.toml, and nothing else.
//
// This is the deterministic half of onboarding. Reception runs the
// conversation (the init-factory skill: what is this factory for, which repos,
// where do merged RFCs land) and then calls this to commit the answers,
// because remembering field names is the part a prompt gets wrong and a binary
// gets right by construction.
//
// It is non-interactive on purpose. Every answer arrives as a flag, the
// rendered config goes to stdout so the caller can read it back before
// committing, and any problem is one line on stderr and a non-zero exit —
// the caller is an agent that has to explain the failure to somebody.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hev/factory/pkg/factory"
)

var (
	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	repoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
	// A Slack conversation id, which is what chat.postMessage wants. A
	// `#channel-name` is the answer somebody gives when they have not opened
	// the channel link yet, and it posts nowhere.
	slackPattern = regexp.MustCompile(`^[CDGU][A-Z0-9]{6,}$`)
	// Deliberately narrower than git allows. A branch name with a space or a
	// `..` in it is a typo every time it reaches this flag, and it would reach
	// the gaffer as a `git fetch` argument.
	branchPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
)

// repoList collects a flag that may be repeated. Commas split too, because the
// caller is usually assembling one command line from a list it already has.
type repoList []string

func (r *repoList) String() string { return strings.Join(*r, ",") }

func (r *repoList) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*r = append(*r, part)
		}
	}
	return nil
}

func runInit(root string, args []string) error {
	if root == "" {
		return fmt.Errorf("no factory checkout found — run ./factory from the repo once, or set FACTORY_ROOT")
	}

	// The package's own error output is discarded so a bad flag is one line
	// rather than one line plus a wall of generated usage; --help still prints
	// the hand-written version below.
	fs := flag.NewFlagSet("factory init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var scope repoList
	fs.Var(&scope, "repo", "a repo this factory works in, owner/name (repeatable)")
	name := fs.String("name", "", "the factory's name; also the config filename")
	plansRepo := fs.String("plans-repo", "", "owner/name of the repo whose plans/active/ holds merged RFCs")
	plansBranch := fs.String("plans-branch", "", "branch on the plans repo the gaffer reads (default: the workspace's current branch)")
	workspace := fs.String("workspace", "", "the tree the gaffer runs in (default: ~/workspace/<plans repo>)")
	runtime := fs.String("runtime", "resident", "resident or one-shot")
	linearTeam := fs.String("linear-team", "", "the Linear team the operator approves RFCs on")
	linearApprovedState := fs.String("linear-approved-state", "", "the workflow state on that team that means approved")
	linearReviewState := fs.String("linear-review-state", "", "the state verified work waits in (default: a testing label)")
	linearBacklogState := fs.String("linear-backlog-state", "", "the state parked work goes to (default: a backlog label)")
	linearMCPServer := fs.String("linear-mcp-server", "", "the MCP server name holding this workspace's Linear login (default: linear)")
	slackWebhook := fs.String("slack-webhook", "", "Slack incoming webhook URL — the simple way to let a factory talk")
	slackChannel := fs.String("slack-channel", "", "Slack channel id, for a bot token instead of a webhook")
	dryRun := fs.Bool("dry-run", false, "render the config to stdout and write nothing")
	force := fs.Bool("force", false, "overwrite an existing config")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Print(initUsage)
			return nil
		}
		return fmt.Errorf("%s — see factory init --help", err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q — every answer is a flag", fs.Arg(0))
	}

	cfg, err := buildInstance(root, answers{
		name:                *name,
		plansRepo:           *plansRepo,
		plansBranch:         *plansBranch,
		scope:               scope,
		workspace:           *workspace,
		runtime:             *runtime,
		linearTeam:          *linearTeam,
		linearApprovedState: *linearApprovedState,
		linearReviewState:   *linearReviewState,
		linearBacklogState:  *linearBacklogState,
		linearMCPServer:     *linearMCPServer,
		slackWebhook:        *slackWebhook,
		slackChannel:        *slackChannel,
	})
	if err != nil {
		return err
	}

	path := filepath.Join(root, "factories", cfg.name+".toml")
	if _, err := os.Stat(path); err == nil && !*dryRun && !*force {
		return fmt.Errorf("factories/%s.toml already exists — pass --force to replace it", cfg.name)
	}

	if clash := channelClash(root, cfg); clash != "" {
		fmt.Fprintln(os.Stderr, clash)
	}

	rendered := cfg.render()
	fmt.Print(rendered)

	if *dryRun {
		fmt.Fprintf(os.Stderr, "dry run — nothing written\n")
		return nil
	}
	if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("could not write factories/%s.toml: %w", cfg.name, err)
	}

	// The credential goes to the keychain, not the file. A machine without
	// `security` says so and names the fallback rather than failing the init:
	// the config it just wrote is correct either way, and a factory whose
	// Slack is not wired up yet is a normal factory.
	if cfg.slackWebhook != "" {
		if err := factory.SecretSet(cfg.name, "SLACK_WEBHOOK_URL", cfg.slackWebhook); err != nil {
			fmt.Fprintf(os.Stderr, "warn: %v\n", err)
			fmt.Fprintf(os.Stderr, "warn: put SLACK_WEBHOOK_URL_%s in ~/.factory/secrets instead\n",
				strings.ToUpper(strings.NewReplacer("-", "_").Replace(cfg.name)))
		} else {
			fmt.Fprintf(os.Stderr, "stored the webhook in the login keychain (%s)\n",
				factory.SecretAccount(cfg.name, "SLACK_WEBHOOK_URL"))
		}
	}

	// Read it back the way the picker and the boot script will. A config that
	// writes but does not parse is the failure this whole subcommand exists to
	// make impossible, so it is worth catching here rather than at boot.
	if !configured(root, cfg.name) {
		return fmt.Errorf("wrote factories/%s.toml but it did not parse back — remove it and try again", cfg.name)
	}

	fmt.Fprintf(os.Stderr, "wrote factories/%s.toml\n", cfg.name)
	return nil
}

// channelClash reports another factory already pointed at this one's channel.
// It is a note rather than a refusal — one shared feed is a choice somebody can
// make — but it is never the arrangement anybody means to end up in, and the
// symptom is two jobs interleaved in a channel with nobody sure which factory
// said what.
func channelClash(root string, cfg *instance) string {
	for _, other := range factory.LoadInstances(root) {
		if other.Name == cfg.name {
			continue
		}
		switch {
		// Other factories' webhooks live in the keychain now, so this asks the
		// keychain. An older config that still carries the field inline is
		// checked too, because a machine mid-migration has both.
		case cfg.slackWebhook != "" &&
			(other.SlackWebhook == cfg.slackWebhook ||
				factory.SecretGet(other.Name, "SLACK_WEBHOOK_URL") == cfg.slackWebhook):
			return fmt.Sprintf("note: %q already posts to this webhook — one channel per factory is the arrangement that reads", other.Name)
		case cfg.slackChannel != "" && other.SlackChannel == cfg.slackChannel:
			return fmt.Sprintf("note: %q already posts to channel %s — one channel per factory is the arrangement that reads", other.Name, cfg.slackChannel)
		}
	}
	return ""
}

// instance is the config as answered, after defaults and validation. It is a
// separate shape from factory.Instance because that one decodes only the two
// fields a viewer needs, and this one has to write every field a gaffer reads.
type instance struct {
	name                string
	workspace           string
	plansRepo           string
	plansBranch         string
	homeHost            string
	loopContract        string
	runtime             string
	linearTeam          string
	linearApprovedState string
	linearReviewState   string
	linearBacklogState  string
	linearMCPServer     string
	slackWebhook        string
	slackChannel        string
	scope               []string
}

// answers is what the init conversation collected. It is a struct rather than
// a positional tail because the tail had reached nine strings, and a caller
// that swaps two of them writes a config that is wrong in a way nothing here
// can detect.
type answers struct {
	name                string
	plansRepo           string
	plansBranch         string
	scope               repoList
	workspace           string
	runtime             string
	linearTeam          string
	linearApprovedState string
	linearReviewState   string
	linearBacklogState  string
	linearMCPServer     string
	slackWebhook        string
	slackChannel        string
}

func buildInstance(root string, a answers) (*instance, error) {
	name, plansRepo, plansBranch := a.name, a.plansRepo, a.plansBranch
	scope, workspace, runtime := a.scope, a.workspace, a.runtime
	slackWebhook, slackChannel := a.slackWebhook, a.slackChannel

	name = strings.ToLower(strings.TrimSpace(name))
	switch {
	case name == "":
		return nil, fmt.Errorf("--name is required: a factory is one workspace and one job, named after the job")
	case name == "example":
		return nil, fmt.Errorf("--name example is the template's name; pick another")
	case !slugPattern.MatchString(name):
		return nil, fmt.Errorf("--name %q must be lowercase letters, digits, and dashes", name)
	}

	// The one field a factory cannot boot without: a gaffer's whole trigger is
	// a merged RFC appearing in this repo's plans/active/.
	plansRepo = strings.TrimSpace(plansRepo)
	if plansRepo == "" {
		return nil, fmt.Errorf("--plans-repo is required: a factory boots from merged RFCs, and this is where they live")
	}
	if !repoPattern.MatchString(plansRepo) {
		return nil, fmt.Errorf("--plans-repo %q must be owner/name", plansRepo)
	}

	for _, repo := range scope {
		if !repoPattern.MatchString(repo) {
			return nil, fmt.Errorf("--repo %q must be owner/name", repo)
		}
	}

	// Linear is where the operator approves, so these are as load-bearing as
	// the plans repo. A config missing either writes fine and then never
	// sees an approval, which reads as a broken factory rather than a
	// half-answered form — so it is refused here instead.
	linearTeam := strings.TrimSpace(a.linearTeam)
	if linearTeam == "" {
		return nil, fmt.Errorf("--linear-team is required: the operator approves RFCs in Linear, and a factory reads one team")
	}
	linearApprovedState := strings.TrimSpace(a.linearApprovedState)
	if linearApprovedState == "" {
		return nil, fmt.Errorf("--linear-approved-state is required: moving an RFC into it is the only thing that approves one — list the team's states and ask which")
	}

	// Optional, and only ever set on a machine holding more than one Linear
	// login. MCP OAuth sessions are keyed by server name, so a second
	// registration of the same URL is how two workspaces coexist.
	linearMCPServer := strings.TrimSpace(a.linearMCPServer)
	if linearMCPServer != "" && !slugPattern.MatchString(linearMCPServer) {
		return nil, fmt.Errorf("--linear-mcp-server %q must be a server name like linear or linear-acme — it is what `claude mcp add` was given", linearMCPServer)
	}

	switch runtime {
	case "resident", "one-shot":
	default:
		return nil, fmt.Errorf("--runtime %q is not resident or one-shot", runtime)
	}

	// A webhook is the whole credential and the channel at once, which is why
	// it is the one the conversation asks for. Anything that is not one of
	// Slack's URLs would fail silently every beat.
	slackWebhook = strings.TrimSpace(slackWebhook)
	if slackWebhook != "" && !strings.HasPrefix(slackWebhook, "https://hooks.slack.com/") {
		return nil, fmt.Errorf("--slack-webhook %q must be a https://hooks.slack.com/... URL — Slack shows it when you add the webhook", slackWebhook)
	}

	// Optional, but a channel that will never accept a post is worse than no
	// channel at all: the beat carries on and the report goes nowhere.
	slackChannel = strings.TrimSpace(slackChannel)
	if slackChannel != "" && !slackPattern.MatchString(strings.ToUpper(slackChannel)) {
		return nil, fmt.Errorf("--slack-channel %q must be a channel id like C0123456789 — open the channel in Slack, Copy link, and take the last path segment", slackChannel)
	}
	slackChannel = strings.ToUpper(slackChannel)

	home, _ := os.UserHomeDir()
	if workspace = strings.TrimSpace(workspace); workspace == "" {
		workspace = defaultWorkspace(home, plansRepo)
	}

	// Resolved once, here, and written into the config. A gaffer that read the
	// checked-out branch every beat would follow a `git checkout` somebody did
	// for an unrelated reason, and the symptom is a factory that quietly stops
	// seeing approved plans.
	plansBranch = strings.TrimSpace(plansBranch)
	if plansBranch == "" {
		plansBranch = currentBranch(expandTilde(home, workspace))
	}
	if !branchPattern.MatchString(plansBranch) {
		return nil, fmt.Errorf("--plans-branch %q is not a branch name", plansBranch)
	}

	return &instance{
		name:                name,
		workspace:           tildeify(home, expandTilde(home, workspace)),
		plansRepo:           plansRepo,
		plansBranch:         plansBranch,
		homeHost:            shortHostname(),
		loopContract:        tildeify(home, filepath.Join(root, "factory-loop.md")),
		runtime:             runtime,
		linearTeam:          linearTeam,
		linearApprovedState: linearApprovedState,
		linearReviewState:   strings.TrimSpace(a.linearReviewState),
		linearBacklogState:  strings.TrimSpace(a.linearBacklogState),
		linearMCPServer:     linearMCPServer,
		slackWebhook:        slackWebhook,
		slackChannel:        slackChannel,
		scope:               withPlansRepo(scope, plansRepo),
	}, nil
}

// withPlansRepo keeps the answered order and makes sure the plans repo is in
// scope. It always is in practice — a gaffer that may not touch the repo it
// reads plans from cannot archive a finished one — so leaving it out is a
// mistake rather than a choice.
func withPlansRepo(scope []string, plansRepo string) []string {
	out := make([]string, 0, len(scope)+1)
	seen := map[string]bool{}
	for _, repo := range append(append([]string{}, scope...), plansRepo) {
		if !seen[repo] {
			seen[repo] = true
			out = append(out, repo)
		}
	}
	return out
}

// defaultWorkspace is where the gaffer runs: the plans repo's tree, because
// that is where plans/active/ and .factory-watermark live. A scope whose repos
// are not on the machine yet still gets a bootable config by falling back to
// the workspace root.
func defaultWorkspace(home, plansRepo string) string {
	base := filepath.Join(home, "workspace")
	if env := os.Getenv("FACTORY_WORKSPACE"); env != "" {
		base = env
	}
	tree := filepath.Join(base, plansRepo[strings.LastIndex(plansRepo, "/")+1:])
	if fi, err := os.Stat(tree); err == nil && fi.IsDir() {
		return tree
	}
	return base
}

func (i *instance) render() string {
	var b strings.Builder
	b.WriteString("# Written by `factory init`. Every field is documented in example.toml.\n")
	fmt.Fprintf(&b, "name           = %q\n", i.name)
	fmt.Fprintf(&b, "workspace_path = %q\n", i.workspace)
	fmt.Fprintf(&b, "plans_repo     = %q\n", i.plansRepo)
	// Frozen at init. The gaffer never re-reads what is checked out, so this
	// line is the only thing that decides which branch approved intent is on.
	fmt.Fprintf(&b, "plans_branch   = %q\n", i.plansBranch)
	// No session field: every session name is <role>-<name> and falls out of
	// the instance (factory-<name>, gaffer-<name>, worker-<name>-…), so there
	// is nothing here that can disagree with itself.
	fmt.Fprintf(&b, "home_host      = %q\n", i.homeHost)
	fmt.Fprintf(&b, "loop_contract  = %q\n", i.loopContract)
	fmt.Fprintf(&b, "runtime        = %q\n", i.runtime)
	// Where the operator acts. Its own block, because the names are longer
	// than the column above and a half-aligned field reads as a typo. The
	// state is the whole approval signal, so it is written verbatim and never
	// normalized — Linear state names carry the operator's own capitalization
	// and spaces.
	b.WriteString("\n")
	fmt.Fprintf(&b, "linear_team           = %q\n", i.linearTeam)
	fmt.Fprintf(&b, "linear_approved_state = %q\n", i.linearApprovedState)
	// Optional, and only written when the team has a state meaning each. A
	// factory on a team without one marks the same thing with a label, so an
	// absent line here is a working config rather than a half-answered one.
	if i.linearReviewState != "" {
		fmt.Fprintf(&b, "linear_review_state   = %q\n", i.linearReviewState)
	}
	if i.linearBacklogState != "" {
		fmt.Fprintf(&b, "linear_backlog_state  = %q\n", i.linearBacklogState)
	}
	if i.linearMCPServer != "" {
		fmt.Fprintf(&b, "linear_mcp_server     = %q\n", i.linearMCPServer)
	}
	b.WriteString("\n")
	// The gaffer's own model. A parent dispatches, verifies and reports; the
	// moments that need more are handed to a subagent or a worker, each of
	// which picks its own. Both runtimes fall back to the same value when the
	// field is absent, so this is written to be read rather than to take effect.
	b.WriteString("model          = \"claude-fable-5\"\n")
	b.WriteString("effort         = \"high\"\n")
	// The webhook is deliberately not here. It goes to the login keychain
	// (internal/factory/secrets.go), and this line says so, because the next
	// person to read the config will want to know where the factory's voice
	// went. A channel id is not a secret and stays.
	if i.slackWebhook != "" {
		fmt.Fprintf(&b, "# slack webhook: login keychain, service hev-factory, account %q\n",
			factory.SecretAccount(i.name, "SLACK_WEBHOOK_URL"))
	}
	if i.slackChannel != "" {
		fmt.Fprintf(&b, "slack_channel  = %q\n", i.slackChannel)
	}
	b.WriteString("\n")
	quoted := make([]string, len(i.scope))
	for n, repo := range i.scope {
		quoted[n] = fmt.Sprintf("%q", repo)
	}
	fmt.Fprintf(&b, "repo_scope = [%s]\n", strings.Join(quoted, ", "))
	return b.String()
}

// configured reports whether an instance of this name reads back out of the
// checkout, using the same loader the picker and the boot script use.
func configured(root, name string) bool {
	for _, inst := range factory.LoadInstances(root) {
		if inst.Name == name {
			return true
		}
	}
	return false
}

// currentBranch is the branch checked out in the workspace, which is the plans
// repo's tree. It is the default because the branch you are standing on is the
// branch you mean, and asking GitHub for a default branch would be a different
// answer on a fork or a repo that never renamed master.
//
// Everything that can go wrong here — no tree yet, not a git repo, a detached
// HEAD — resolves to the same place: `main`, written into the config where it
// is visible and editable. An init that failed because a directory was missing
// would be a worse answer than a line somebody can correct.
func currentBranch(workspace string) string {
	if workspace == "" {
		return factory.DefaultPlansBranch
	}
	out, err := exec.Command("git", "-C", workspace, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return factory.DefaultPlansBranch
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return factory.DefaultPlansBranch
	}
	return branch
}

// shortHostname matches what `hostname -s` gives the shell scripts, which is
// what home_host is compared against at boot.
func shortHostname() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "this-machine"
	}
	if dot := strings.Index(host, "."); dot > 0 {
		host = host[:dot]
	}
	return strings.ToLower(host)
}

func expandTilde(home, path string) string {
	if home == "" || path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
}

// tildeify keeps written configs portable between accounts, and matches how
// example.toml reads.
func tildeify(home, path string) string {
	if home == "" || !strings.HasPrefix(path, home) {
		return path
	}
	return "~" + strings.TrimPrefix(path, home)
}

const initUsage = `factory init — write one factory's config

  factory init --name acme --plans-repo acme/api \
               --repo acme/api --repo acme/docs \
               --linear-team ENG --linear-approved-state "Ready to start"

  --name          the factory's name; also factories/<name>.toml
  --plans-repo    owner/name whose plans/active/ holds approved RFCs (required)
  --plans-branch  branch the gaffer reads (default: the workspace's branch)
  --repo          a repo this factory works in (repeatable, or comma-separated)
  --workspace     the tree the gaffer runs in
  --runtime       resident (default) or one-shot
  --linear-team            the Linear team the operator approves on (required)
  --linear-approved-state  the state that means approved (required)
  --linear-review-state    state verified work waits in (else a testing label)
  --linear-backlog-state   state parked work goes to (else a backlog label)
  --linear-mcp-server      MCP server holding this Linear login (default: linear)
  --slack-webhook Slack incoming webhook URL: where this factory talks
  --slack-channel channel id, only if you are using a bot token instead
  --dry-run       render to stdout, write nothing
  --force         overwrite an existing config

The rendered config always goes to stdout, so --dry-run is how you show
somebody what is about to be written.
`
