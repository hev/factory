package factory

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Instance is one configured factory — factories/<name>.toml, as the Go tools
// read it. Session names are not in here: they fall out of the name
// (ReceptionFor, GafferFor, WorkerPrefix), so a config cannot name a session
// that disagrees with its own instance. The gaffer scripts read the fields
// nothing here needs.
type Instance struct {
	Name          string `toml:"name"`
	WorkspacePath string `toml:"workspace_path"`
	PlansRepo     string `toml:"plans_repo"`
	PlansBranch   string `toml:"plans_branch"`
	Runtime       string `toml:"runtime"`
	HomeHost      string `toml:"home_host"`
	Model         string `toml:"model"`
	// Where the operator approves. The team is the scope wall in Linear that
	// repo_scope is on GitHub, and the state is the entire approval signal.
	LinearTeam          string `toml:"linear_team"`
	LinearApprovedState string `toml:"linear_approved_state"`
	// Optional. Where verified work waits and where parked work goes, when the
	// team already has a state meaning each. Absent means the factory marks it
	// with its own label instead, which is what a team with no equivalent gets.
	LinearReviewState  string `toml:"linear_review_state"`
	LinearBacklogState string `toml:"linear_backlog_state"`
	LinearMCPServer    string `toml:"linear_mcp_server"`
	// Where this factory talks. One channel per factory, so these are the
	// fields `factory init` checks a new config against.
	SlackWebhook string `toml:"slack_webhook"`
	SlackChannel string `toml:"slack_channel"`
}

// DefaultModel is what both runtimes fall back to when a config names none.
// Kept in step with factory-up.sh and factory-iterate.sh.
const DefaultModel = "claude-fable-5"

// DefaultLinearMCPServer is the MCP server a config names when it names none.
// A machine with one Linear login never sets the field; one holding two gives
// each factory the server name its own workspace was registered under, because
// MCP OAuth sessions are keyed by server name rather than by URL.
const DefaultLinearMCPServer = "linear"

// LinearServer is the MCP server this factory's Linear calls go through.
func (i Instance) LinearServer() string {
	if i.LinearMCPServer == "" {
		return DefaultLinearMCPServer
	}
	return i.LinearMCPServer
}

// DefaultPlansBranch is the branch a config written before plans_branch
// existed means. `factory init` always writes the field now, so this only
// covers configs older than the field — and it is the value they were built
// against, so nothing moves under them.
const DefaultPlansBranch = "main"

// Branch is the ref on plans_repo the gaffer treats as approved intent. The
// branch is frozen in the config rather than read from whatever is checked out,
// so a stray `git checkout` in the workspace cannot redirect a factory.
func (i Instance) Branch() string {
	if i.PlansBranch == "" {
		return DefaultPlansBranch
	}
	return i.PlansBranch
}

// Workspace is the tree the gaffer runs in, with ~ expanded.
func (i Instance) Workspace() string {
	if i.WorkspacePath == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(i.WorkspacePath, "~/") {
		return filepath.Join(home, i.WorkspacePath[2:])
	}
	return i.WorkspacePath
}

// AtHome reports whether this machine is the one the config allows the factory
// to run on. A stale clone on a laptop is quiet precisely because this is false.
func (i Instance) AtHome() bool {
	host, err := os.Hostname()
	if err != nil {
		return false
	}
	if dot := strings.Index(host, "."); dot > 0 {
		host = host[:dot]
	}
	return strings.EqualFold(host, i.HomeHost)
}

// LoadInstances reads every factories/*.toml under root, skipping example.toml
// (the template, which names a factory that does not exist). A config that
// fails to parse is skipped rather than fatal: the picker's job is to show what
// is running, and it should still do that next to a half-edited config.
func LoadInstances(root string) []Instance {
	if root == "" {
		return nil
	}
	paths, err := filepath.Glob(filepath.Join(root, "factories", "*.toml"))
	if err != nil {
		return nil
	}
	var out []Instance
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".toml")
		if name == "example" {
			continue
		}
		var inst Instance
		if _, err := toml.DecodeFile(path, &inst); err != nil {
			continue
		}
		if inst.Name == "" {
			inst.Name = name
		}
		if inst.Runtime == "" {
			inst.Runtime = "resident"
		}
		if inst.Model == "" {
			inst.Model = DefaultModel
		}
		out = append(out, inst)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// LedgerDir is where the child ledger lives (contracts/child-ledger.md). It is
// machine-local: children run where their tmux session runs.
func LedgerDir() string {
	if d := os.Getenv("FACTORY_LEDGER_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".factory", "children")
}
