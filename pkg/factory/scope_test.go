package factory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFactory lays out a repo root with the instance configs a test needs.
func writeFactory(t *testing.T, configs map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "factories"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range configs {
		if err := os.WriteFile(filepath.Join(root, "factories", name+".toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const acmeConfig = `
name           = "acme"
workspace_path = "~/workspace/acme"
home_host      = "a-mac-mini"
repo_scope     = ["acme/api"]
`

func TestClassifyKeepsTheFactoryAndNothingElse(t *testing.T) {
	root := writeFactory(t, map[string]string{
		"acme":    acmeConfig,
		"example": "name = \"example\"\n",
	})
	t.Setenv("FACTORY_LEDGER_DIR", t.TempDir())

	scope := NewScope(root)
	if len(scope.Instances) != 1 {
		t.Fatalf("example.toml is the template and must not count as a factory: got %d instances", len(scope.Instances))
	}

	now := time.Now()
	for name, want := range map[string]Kind{
		"reception":             NotFactory,
		"factory-acme":          NotFactory,
		"gaffer-acme":           Gaffer,
		"worker-acme-search":    Worker,     // worker-<instance>-<slug>, no ledger file
		"worker-acme-14-search": Worker,     // an old issue-numbered name still reads
		"factory-example":       NotFactory, // configured only in the template
		"gaffer-example":        NotFactory,
		"factory-view":          NotFactory, // no instance by that name
		"worker-acme":           NotFactory, // the prefix with nothing after it
		"acme-14-search":        NotFactory, // the old convention, now somebody's shell
		"acme":                  NotFactory,
		"bcc-docs":              NotFactory,
		"worker-bcc-14-thing":   NotFactory, // an unconfigured instance
		"strategy":              NotFactory,
	} {
		if got := scope.Classify(name, now).Kind; got != want {
			t.Errorf("Classify(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestClassifyReadsTheLedger(t *testing.T) {
	root := writeFactory(t, map[string]string{"acme": acmeConfig})
	ledger := t.TempDir()
	t.Setenv("FACTORY_LEDGER_DIR", ledger)

	dispatched := time.Now().Add(-9 * time.Hour).UTC().Format(time.RFC3339)
	write := func(session, body string) {
		if err := os.WriteFile(filepath.Join(ledger, session+".json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("worker-acme-14-search", `{"session":"worker-acme-14-search","instance":"acme","repo":"acme/api","issue":14,
		"rfc":"agent-ready-pr-handoff","dispatched_at":"`+dispatched+`"}`)
	write("worker-acme-15-index", `{"session":"worker-acme-15-index","instance":"acme","repo":"acme/api","issue":"15",
		"plan":"hev-agentic-gates","pr":41,"dispatched_at":"`+dispatched+`"}`)
	// A worker whose name carries no instance at all is still the factory's
	// when the gaffer wrote it down.
	write("nightly", `{"session":"nightly","instance":"acme","repo":"acme/api","issue":7,
		"dispatched_at":"`+time.Now().UTC().Format(time.RFC3339)+`"}`)

	scope := NewScope(root)
	now := time.Now()

	search := scope.Classify("worker-acme-14-search", now)
	if search.Kind != Worker || search.Instance != "acme" || search.Issue != "14" {
		t.Errorf("ledger worker mislabelled: %+v", search)
	}
	if search.Tag != "agent-ready-pr-handoff" {
		t.Errorf("rfc should be the tag, got %q", search.Tag)
	}
	if !search.Stale {
		t.Error("9h old with no PR should be stale")
	}

	index := scope.Classify("worker-acme-15-index", now)
	if index.Issue != "15" {
		t.Errorf("a string issue number should decode, got %q", index.Issue)
	}
	if index.Tag != "~hev-agentic-gates" {
		t.Errorf("a plan tag wears a ~, got %q", index.Tag)
	}
	if index.Stale {
		t.Error("a worker with a PR is never stale, however old")
	}

	if got := scope.Classify("nightly", now); got.Kind != Worker || got.Issue != "7" {
		t.Errorf("a ledger entry outranks the naming convention: %+v", got)
	}
}

func TestStaleThresholdIsConfigurable(t *testing.T) {
	t.Setenv("LEDGER_STALE_HOURS", "1")
	if got := StaleThreshold(); got != time.Hour {
		t.Errorf("StaleThreshold() = %v, want 1h", got)
	}
	t.Setenv("LEDGER_STALE_HOURS", "not-a-number")
	if got := StaleThreshold(); got != DefaultStaleHours*time.Hour {
		t.Errorf("a junk value should fall back to the default, got %v", got)
	}
}

func TestSessionsFiltersInPlace(t *testing.T) {
	root := writeFactory(t, map[string]string{"acme": acmeConfig})
	t.Setenv("FACTORY_LEDGER_DIR", t.TempDir())

	kept := NewScope(root).Sessions(
		[]string{"reception", "bcc-docs", "gaffer-acme", "strategy", "worker-acme-14-x"}, time.Now())
	if len(kept) != 2 {
		t.Fatalf("kept %d sessions, want 2: %+v", len(kept), kept)
	}
	for _, name := range []string{"gaffer-acme", "worker-acme-14-x"} {
		if _, ok := kept[name]; !ok {
			t.Errorf("%q should have been kept", name)
		}
	}
}

// Issues live in Linear, so an identifier is text and not a number. A ledger
// carrying `HEV-14` has to survive the trip; the old bare-number form still
// decodes, because entries written before the move are still on disk.
func TestClassifyKeepsALinearIdentifier(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FACTORY_LEDGER_DIR", root)
	dispatched := time.Now().UTC().Format(time.RFC3339)

	write := func(session, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, session+".json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("worker-acme-search", `{"session":"worker-acme-search","instance":"acme","repo":"acme/api",
		"issue":"HEV-14","plan":"search","pr":9,"dispatched_at":"`+dispatched+`"}`)

	scope := NewScope(root)
	if got := scope.Classify("worker-acme-search", time.Now()); got.Issue != "HEV-14" {
		t.Errorf("Issue = %q, want HEV-14", got.Issue)
	}
}
