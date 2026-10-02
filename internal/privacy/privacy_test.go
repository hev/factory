package privacy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (string, string, Plan) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, ".privacy-offline"), nil, 0600)
	p := Plan{Version: 1}
	for _, name := range []string{"sessions/abcdef/log.jsonl", "jobs/ghijkl/log.md"} {
		b := []byte("header\nsynthetic private narrative\n")
		if strings.HasSuffix(name, "jsonl") {
			b = []byte("{\"type\":\"message\",\"text\":\"synthetic private narrative\"}\n")
		}
		path := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(path), 0700)
		os.WriteFile(path, b, 0640)
		start := strings.Index(string(b), "private")
		p.Targets = append(p.Targets, Target{Path: name, Before: digest(b), Ranges: []Range{{start, start + 7}}})
	}
	input := filepath.Join(t.TempDir(), "request.json")
	b, _ := json.Marshal(p)
	os.WriteFile(input, b, 0600)
	return root, input, p
}
func TestPlanApplyRecoveryAndIntegrity(t *testing.T) {
	root, input, _ := fixture(t)
	plan := filepath.Join(t.TempDir(), "plan")
	p, e := Execute("plan", root, input, plan)
	if e != nil {
		t.Fatal(e)
	}
	for _, target := range p.Targets {
		b, _ := os.ReadFile(filepath.Join(root, target.Path))
		if digest(b) != target.Before {
			t.Fatal("dry run wrote")
		}
	}
	// Simulate interruption after one independent artifact replacement.
	target := p.Targets[0]
	path := filepath.Join(root, target.Path)
	b, _ := os.ReadFile(path)
	out, e := transform(b, target)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(path, out, 0640)
	receipt := filepath.Join(t.TempDir(), "receipt")
	got, e := Execute("apply", root, plan, receipt)
	if e != nil {
		t.Fatal(e)
	}
	if got.Complete || got.Index != "unknown" || got.Native != "unsupported" {
		t.Fatal("coverage")
	}
	for _, target := range got.Targets {
		b, _ := os.ReadFile(filepath.Join(root, target.Path))
		if digest(b) != target.After || strings.Contains(string(b), "private") {
			t.Fatal("copy not remediated")
		}
		s, _ := os.Stat(filepath.Join(root, target.Path))
		if s.Mode().Perm() != 0640 {
			t.Fatal("permissions")
		}
	}
	r, _ := os.ReadFile(receipt)
	if strings.Contains(string(r), "narrative") {
		t.Fatal("receipt leaked")
	}
	if _, e = Execute("apply", root, plan, filepath.Join(t.TempDir(), "retry")); e != nil {
		t.Fatal(e)
	}
}
func TestFailClosed(t *testing.T) {
	for _, kind := range []string{"traversal", "unsupported", "duplicate", "overlap", "stale", "unbounded", "index", "native", "symlink", "active", "utf8", "key", "scalar"} {
		t.Run(kind, func(t *testing.T) {
			root, input, p := fixture(t)
			mode := "plan"
			switch kind {
			case "traversal":
				p.Targets[0].Path = "../log.md"
			case "unsupported":
				p.Targets[0].Path = "sessions/abcdef/state.json"
			case "duplicate":
				p.Targets = append(p.Targets, p.Targets[0])
			case "overlap":
				p.Targets[0].Ranges = append(p.Targets[0].Ranges, p.Targets[0].Ranges[0])
			case "stale":
				p.Targets[0].Before = strings.Repeat("0", 64)
			case "unbounded":
				p.Targets[0].Ranges[0].End = 1 << 30
			case "index":
				mode = "apply"
				p.Index = "supported"
			case "native":
				mode = "apply"
				p.Native = "supported"
			case "symlink":
				path := filepath.Join(root, p.Targets[0].Path)
				os.Remove(path)
				os.Symlink(input, path)
			case "active":
				os.Remove(filepath.Join(root, ".privacy-offline"))
			case "utf8":
				path := filepath.Join(root, p.Targets[1].Path)
				b := []byte("é\n")
				os.WriteFile(path, b, 0600)
				p.Targets[1].Before = digest(b)
				p.Targets[1].Ranges = []Range{{1, 2}}
			case "key":
				p.Targets[0].Ranges = []Range{{2, 6}}
			case "scalar":
				p.Targets[0].Ranges = []Range{{9, 16}}
			}
			b, _ := json.Marshal(p)
			os.WriteFile(input, b, 0600)
			_, e := Execute(mode, root, input, filepath.Join(t.TempDir(), "output"))
			if e == nil {
				t.Fatal("accepted unsafe request")
			}
			if strings.Contains(e.Error(), "narrative") {
				t.Fatal("error leaked")
			}
		})
	}
}
func TestChangedCopyPreventsAllWrites(t *testing.T) {
	root, input, p := fixture(t)
	plan := filepath.Join(t.TempDir(), "plan")
	if _, e := Execute("plan", root, input, plan); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root, p.Targets[1].Path), []byte("independent append\n"), 0600)
	if _, e := Execute("apply", root, plan, filepath.Join(t.TempDir(), "receipt")); e == nil {
		t.Fatal("accepted append")
	}
	b, _ := os.ReadFile(filepath.Join(root, p.Targets[0].Path))
	if digest(b) != p.Targets[0].Before {
		t.Fatal("partial validation wrote")
	}
}

func TestJSONAmbiguityAndEscapeBoundaries(t *testing.T) {
	for _, body := range []string{`{"text":"private","text":"private"}`, `{"text":"\u0070rivate"}`} {
		b := append([]byte(body), '\n')
		start := strings.Index(body, "private")
		if start < 0 {
			start = strings.Index(body, "0070")
		}
		target := Target{Path: "sessions/abcdef/log.jsonl", Ranges: []Range{{start, start + 1}}}
		if _, e := transform(b, target); e == nil {
			t.Fatal("ambiguous or escape target accepted")
		}
	}
}
func TestNoChangesToOperationalJSONFields(t *testing.T) {
	b := []byte("{\"type\":\"message\",\"text\":\"private\",\"turn\":1}\n")
	start := strings.Index(string(b), "message")
	if _, e := transform(b, Target{Path: "sessions/abcdef/log.jsonl", Ranges: []Range{{start, start + 7}}}); e == nil {
		t.Fatal("operational field changed")
	}
}

func TestReceiptFailureRecovery(t *testing.T) {
	root, input, _ := fixture(t)
	plan := filepath.Join(t.TempDir(), "plan")
	if _, e := Execute("plan", root, input, plan); e != nil {
		t.Fatal(e)
	}
	// Receipt creation fails after the artifact renames have completed.
	if _, e := Execute("apply", root, plan, filepath.Join(t.TempDir(), "missing", "receipt")); e == nil {
		t.Fatal("receipt failure not reported")
	}
	if _, e := Execute("apply", root, plan, filepath.Join(t.TempDir(), "recovered")); e != nil {
		t.Fatal(e)
	}
}
func TestRejectHardLinks(t *testing.T) {
	root, input, p := fixture(t)
	if e := os.Link(filepath.Join(root, p.Targets[0].Path), filepath.Join(root, "extra-copy")); e != nil {
		t.Fatal(e)
	}
	if _, e := Execute("plan", root, input, filepath.Join(t.TempDir(), "plan")); e == nil {
		t.Fatal("hard-linked target accepted")
	}
}

func TestJobEntryDelimitersPreserved(t *testing.T) {
	b := []byte("## 2026-01-01 00:00:00Z · fixture\n\nprivate narrative\n\n")
	if _, e := transform(b, Target{Path: "jobs/abcdef/log.md", Ranges: []Range{{3, 7}}}); e == nil {
		t.Fatal("entry header changed")
	}
	start := strings.Index(string(b), "private")
	out, e := transform(b, Target{Path: "jobs/abcdef/log.md", Ranges: []Range{{start, start + 7}}})
	if e != nil {
		t.Fatal(e)
	}
	if string(out[:start]) != string(b[:start]) || len(out) != len(b) {
		t.Fatal("entry integrity")
	}
}
