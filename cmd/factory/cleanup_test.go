package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCleanupFixture builds a machine with one configured factory and the
// state a factory accumulates, and returns the checkout root.
func writeCleanupFixture(t *testing.T) (root, home, workspace string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("FACTORY_LEDGER_DIR", filepath.Join(home, ".factory", "children"))

	root = filepath.Join(home, "checkout")
	workspace = filepath.Join(home, "workspace", "acme")

	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(filepath.Join(root, "factories", "acme.toml"),
		"name = \"acme\"\nworkspace_path = \"~/workspace/acme\"\nplans_repo = \"acme/api\"\n")
	write(filepath.Join(root, "factories", "other.toml"),
		"name = \"other\"\nworkspace_path = \"~/workspace/other\"\nplans_repo = \"other/api\"\n")
	write(filepath.Join(workspace, ".factory-watermark"), "deadbeef\n")

	dot := filepath.Join(home, ".factory")
	write(filepath.Join(dot, "heartbeat", "acme"), "")
	write(filepath.Join(dot, "beats", "acme.jsonl"), "{}\n")
	write(filepath.Join(dot, "briefs", "acme", "1-thing.md"), "brief\n")
	write(filepath.Join(dot, "harvest", "acme", "worker-acme-thing.log"), "pane\n")
	write(filepath.Join(dot, "reception", "acme", "notes.md"), "notes\n")
	write(filepath.Join(dot, "reception", "notes.md"), "bootstrap desk\n")
	write(filepath.Join(dot, "children", "worker-acme-thing.json"), `{"instance":"acme"}`)
	write(filepath.Join(dot, "children", "worker-other-thing.json"), `{"instance":"other"}`)
	write(filepath.Join(dot, "root"), root+"\n")

	// Another factory's state, which a scoped cleanup must not reach.
	write(filepath.Join(dot, "heartbeat", "other"), "")
	write(filepath.Join(dot, "beats", "other.jsonl"), "{}\n")
	return root, home, workspace
}

func planPaths(plan cleanupPlan) []string {
	out := make([]string, 0, len(plan.paths))
	for _, item := range plan.paths {
		out = append(out, item.path)
	}
	return out
}

func TestCleanupPlanTakesOneFactoryAndLeavesTheRest(t *testing.T) {
	root, home, workspace := writeCleanupFixture(t)
	dot := filepath.Join(home, ".factory")

	got := planPaths(buildCleanupPlan(root, []string{"acme"}, false))

	for _, want := range []string{
		filepath.Join(root, "factories", "acme.toml"),
		filepath.Join(dot, "heartbeat", "acme"),
		filepath.Join(dot, "beats", "acme.jsonl"),
		filepath.Join(dot, "briefs", "acme"),
		filepath.Join(dot, "harvest", "acme"),
		filepath.Join(dot, "reception", "acme"),
		filepath.Join(dot, "children", "worker-acme-thing.json"),
		filepath.Join(workspace, ".factory-watermark"),
	} {
		if !contains(got, want) {
			t.Errorf("plan should remove %s\ngot: %s", want, strings.Join(got, "\n     "))
		}
	}

	// Another factory's state, the bootstrap desk, and the checkout pointer are
	// all somebody else's business.
	for _, unwanted := range []string{
		filepath.Join(root, "factories", "other.toml"),
		filepath.Join(dot, "heartbeat", "other"),
		filepath.Join(dot, "beats", "other.jsonl"),
		filepath.Join(dot, "children", "worker-other-thing.json"),
		filepath.Join(dot, "reception", "notes.md"),
		filepath.Join(dot, "reception"),
		filepath.Join(dot, "root"),
		dot,
		home,
	} {
		if contains(got, unwanted) {
			t.Errorf("plan must not remove %s", unwanted)
		}
	}
}

func TestCleanupAllTakesTheBootstrapDeskToo(t *testing.T) {
	root, home, _ := writeCleanupFixture(t)
	dot := filepath.Join(home, ".factory")

	plan := buildCleanupPlan(root, nil, true)
	got := planPaths(plan)

	if len(plan.instances) != 2 {
		t.Errorf("--all should cover every configured factory, got %v", plan.instances)
	}
	for _, want := range []string{
		filepath.Join(root, "factories", "acme.toml"),
		filepath.Join(root, "factories", "other.toml"),
		filepath.Join(dot, "reception"),
	} {
		if !contains(got, want) {
			t.Errorf("--all should remove %s\ngot: %s", want, strings.Join(got, "\n     "))
		}
	}
	// The checkout pointer survives every teardown: it is how the next run
	// finds this repo at all.
	if contains(got, filepath.Join(dot, "root")) {
		t.Error("--all must not remove ~/.factory/root")
	}
	// Nothing above ~/.factory, ever.
	for _, path := range got {
		if path == dot || path == home {
			t.Fatalf("plan reaches too far up: %s", path)
		}
		inDot := strings.HasPrefix(path, dot+string(filepath.Separator))
		inRoot := strings.HasPrefix(path, root+string(filepath.Separator))
		isWatermark := filepath.Base(path) == ".factory-watermark"
		if !inDot && !inRoot && !isWatermark {
			t.Errorf("path outside the allowlist: %s", path)
		}
	}
}

func TestCleanupWatermarkComesFromTheConfig(t *testing.T) {
	root, _, workspace := writeCleanupFixture(t)

	if got := watermarkFor(root, "acme"); got != filepath.Join(workspace, ".factory-watermark") {
		t.Errorf("watermarkFor = %q, want the workspace's file", got)
	}
	if got := watermarkFor(root, "nosuch"); got != "" {
		t.Errorf("an unknown instance has no watermark, got %q", got)
	}
}

func TestListDescribesWhatTheConfigSaysAndTheBeatLogKnows(t *testing.T) {
	root, home, _ := writeCleanupFixture(t)

	byName := map[string]factoryRow{}
	for _, inst := range factoryInstances(root) {
		row := describeInstance(root, inst)
		byName[row.name] = row
	}

	acme, ok := byName["acme"]
	if !ok {
		t.Fatalf("acme should be listed, got %v", byName)
	}
	if acme.plans != "acme/api" {
		t.Errorf("plans repo = %q, want acme/api", acme.plans)
	}
	if acme.runtime != "resident" {
		t.Errorf("a config with no runtime should read as resident, got %q", acme.runtime)
	}
	// The fixture wrote beats/acme.jsonl, so acme has beaten and other has not.
	if acme.lastBeat == "never" {
		t.Error("acme has a beat log and should not read as never")
	}
	// A factory with no beat log has never finished an iteration, whatever its
	// heartbeat says — the fixture leaves other's heartbeat in place to prove
	// the two are read separately.
	if err := os.Remove(filepath.Join(home, ".factory", "beats", "other.jsonl")); err != nil {
		t.Fatal(err)
	}
	for _, inst := range factoryInstances(root) {
		if inst.Name != "other" {
			continue
		}
		if got := describeInstance(root, inst).lastBeat; got != "never" {
			t.Errorf("a factory with no beat log should read as never, got %q", got)
		}
	}
	// home_host is unset in the fixture, so no machine is its home.
	if acme.atHome {
		t.Error("a config with no home_host must not claim this machine")
	}
	if acme.host != "—" {
		t.Errorf("host = %q, want the em dash for an unset home_host", acme.host)
	}
}
