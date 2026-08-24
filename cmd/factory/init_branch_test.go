package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hev/factory/pkg/factory"
)

// gitRepoOn makes a real repository with one commit on the named branch. The
// resolver shells out to git, so a fixture that fakes it would be testing the
// fake.
func gitRepoOn(t *testing.T, dir, branch string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "seed"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "seed")
	run("commit", "-qm", "seed")
}

// acmeAnswers is a complete, valid set of init answers with the fields a test
// does not care about already filled in. Tests tweak the one field they are
// about, so a new required field lands here once rather than in every call.
func acmeAnswers(tweak ...func(*answers)) answers {
	a := answers{
		name:                "acme",
		plansRepo:           "acme/api",
		runtime:             "resident",
		linearTeam:          "ENG",
		linearApprovedState: "Ready to start",
	}
	for _, f := range tweak {
		f(&a)
	}
	return a
}

// The default is the branch you are standing on, which is the whole point of
// not hardcoding main.
func TestInitTakesTheBranchTheWorkspaceIsOn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace", "api")
	gitRepoOn(t, workspace, "factory-next")

	cfg, err := buildInstance(home, acmeAnswers(func(a *answers) { a.workspace = workspace }))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.plansBranch != "factory-next" {
		t.Fatalf("plans_branch = %q, want factory-next", cfg.plansBranch)
	}
	if !strings.Contains(cfg.render(), `plans_branch   = "factory-next"`) {
		t.Fatalf("rendered config does not carry the branch:\n%s", cfg.render())
	}
}

// An explicit flag beats the checkout, because a factory can read a branch
// that is not on this machine at all.
func TestInitPrefersTheExplicitBranch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace", "api")
	gitRepoOn(t, workspace, "factory-next")

	cfg, err := buildInstance(home, acmeAnswers(func(a *answers) { a.workspace = workspace; a.plansBranch = "release" }))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.plansBranch != "release" {
		t.Fatalf("plans_branch = %q, want release", cfg.plansBranch)
	}
}

// A workspace that is not a git tree yet still has to produce a bootable
// config: the branch is a line somebody can correct, not a reason to fail.
func TestInitFallsBackWhenThereIsNoTree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := buildInstance(home, acmeAnswers(func(a *answers) { a.workspace = filepath.Join(home, "nowhere") }))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.plansBranch != factory.DefaultPlansBranch {
		t.Fatalf("plans_branch = %q, want %q", cfg.plansBranch, factory.DefaultPlansBranch)
	}
}

func TestInitRejectsABranchThatIsNotOne(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, err := buildInstance(home, acmeAnswers(func(a *answers) { a.workspace = filepath.Join(home, "w"); a.plansBranch = "not a branch" })); err == nil {
		t.Fatal("expected an error for a branch name with spaces in it")
	}
}

// A config written before plans_branch existed means main, and reads back that
// way rather than as an empty ref.
func TestBranchDefaultsForAConfigWithoutTheField(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "factories"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "factories", "old.toml"),
		[]byte("name = \"old\"\nworkspace_path = \"~/workspace/old\"\nplans_repo = \"acme/api\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	instances := factory.LoadInstances(root)
	if len(instances) != 1 {
		t.Fatalf("loaded %d instances, want 1", len(instances))
	}
	if got := instances[0].Branch(); got != "main" {
		t.Fatalf("Branch() = %q, want main", got)
	}
}

// No Linear at all is a working factory: the operator approves by merging the
// pull request that adds the plan. Half a Linear config is not — every one of
// these names a door with no handle, and fails the way a missing team used to.
func TestInitRefusesHalfALinearConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	for _, tc := range []struct {
		name  string
		tweak func(*answers)
	}{
		{"team, no approved state", func(a *answers) { a.linearApprovedState = "" }},
		{"approved state, no team", func(a *answers) { a.linearTeam = "" }},
		{"review state, no team", func(a *answers) {
			a.linearTeam, a.linearApprovedState, a.linearReviewState = "", "", "In Review"
		}},
		{"backlog state, no team", func(a *answers) {
			a.linearTeam, a.linearApprovedState, a.linearBacklogState = "", "", "Backlog"
		}},
		{"mcp server, no team", func(a *answers) {
			a.linearTeam, a.linearApprovedState, a.linearMCPServer = "", "", "linear-acme"
		}},
	} {
		if _, err := buildInstance(home, acmeAnswers(tc.tweak)); err == nil {
			t.Fatalf("%s: expected an error", tc.name)
		}
	}
}

// The pull-request door. A config with no linear_* flags builds, and renders
// with no linear_ block at all — the gaffer reads that absence as "approval is
// a merged pull request" (contracts/approvals.md), so a stray key would point
// it at a board that is not there.
func TestInitWritesAFactoryWithNoLinear(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := buildInstance(home, acmeAnswers(func(a *answers) {
		a.linearTeam, a.linearApprovedState = "", ""
	}))
	if err != nil {
		t.Fatalf("a factory without Linear should build: %v", err)
	}
	rendered := cfg.render()
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "linear_") {
			t.Fatalf("rendered a linear_ key on a factory with no Linear:\n%s", rendered)
		}
	}
	// The absence is a decision, and the config says so rather than reading
	// like a field somebody forgot.
	if !strings.Contains(rendered, "plans/active/<slug>.md") {
		t.Fatalf("rendered config does not say how approval works:\n%s", rendered)
	}
	// Everything downstream of the branch is unchanged, so the fields the
	// gaffer actually boots on have to still be there.
	for _, want := range []string{`plans_repo     = "acme/api"`, `plans_branch   = `} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered config is missing %s:\n%s", want, rendered)
		}
	}
}

// A state name is the operator's own words — spaces and capitals included —
// so it is written back verbatim rather than slugged.
func TestInitWritesTheApprovedStateVerbatim(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := buildInstance(home, acmeAnswers(func(a *answers) {
		a.linearApprovedState = "Ready to start"
		a.linearTeam = "ENG"
	}))
	if err != nil {
		t.Fatal(err)
	}
	rendered := cfg.render()
	for _, want := range []string{
		`linear_team           = "ENG"`,
		`linear_approved_state = "Ready to start"`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered config is missing %s:\n%s", want, rendered)
		}
	}
	// Absent by default: a machine with one Linear login never names a server.
	if strings.Contains(rendered, "linear_mcp_server") {
		t.Fatalf("rendered a server name nobody asked for:\n%s", rendered)
	}
}

// The second-workspace path: the server name is what `claude mcp add` was
// given, and it only appears when somebody passed one.
func TestInitCarriesASecondLinearLogin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := buildInstance(home, acmeAnswers(func(a *answers) { a.linearMCPServer = "linear-acme" }))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg.render(), `linear_mcp_server     = "linear-acme"`) {
		t.Fatalf("rendered config does not carry the server:\n%s", cfg.render())
	}
	if _, err := buildInstance(home, acmeAnswers(func(a *answers) { a.linearMCPServer = "not a server name" })); err == nil {
		t.Fatal("expected an error for a server name with spaces in it")
	}
}

// A config with no linear_mcp_server means the plain `linear` registration,
// which is what every single-workspace machine has.
func TestLinearServerDefaults(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "factories"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "factories", "acme.toml"),
		[]byte("name = \"acme\"\nplans_repo = \"acme/api\"\nlinear_team = \"ENG\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	instances := factory.LoadInstances(root)
	if len(instances) != 1 {
		t.Fatalf("loaded %d instances, want 1", len(instances))
	}
	if got := instances[0].LinearServer(); got != factory.DefaultLinearMCPServer {
		t.Fatalf("LinearServer() = %q, want %q", got, factory.DefaultLinearMCPServer)
	}
	if got := instances[0].LinearTeam; got != "ENG" {
		t.Fatalf("LinearTeam = %q, want ENG", got)
	}
}

// The queue states are optional: a team with no review or backlog state gets
// labels instead, and the config says so by leaving the lines out.
func TestInitLeavesTheQueueStatesOutWhenUnanswered(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := buildInstance(home, acmeAnswers())
	if err != nil {
		t.Fatal(err)
	}
	rendered := cfg.render()
	for _, absent := range []string{"linear_review_state", "linear_backlog_state"} {
		if strings.Contains(rendered, absent) {
			t.Fatalf("rendered %s with no answer for it:\n%s", absent, rendered)
		}
	}
}

// Answered, they are written verbatim — these are the team's own words for its
// own columns, which is the whole reason for reusing them.
func TestInitCarriesTheQueueStates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := buildInstance(home, acmeAnswers(func(a *answers) {
		a.linearReviewState = "In Review"
		a.linearBacklogState = "Backlog"
	}))
	if err != nil {
		t.Fatal(err)
	}
	rendered := cfg.render()
	for _, want := range []string{
		`linear_review_state   = "In Review"`,
		`linear_backlog_state  = "Backlog"`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered config is missing %s:\n%s", want, rendered)
		}
	}
}
