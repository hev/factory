package factory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// GafferMsg runs the real script, because the whole point of running it rather
// than writing the file is that there is one implementation. So does this: a
// test that wrote its own JSON would pass while the script was broken.
func TestGafferMsgWritesTheInbox(t *testing.T) {
	root := repoRoot(t)
	inbox := t.TempDir()
	t.Setenv("FACTORY_INBOX_DIR", inbox)

	body := "npm test has failed the same way three times\n\nsession: worker-acme-index"
	if err := GafferMsg(root, "acme", body); err != nil {
		t.Fatalf("GafferMsg: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(inbox, "acme", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("want one message in the inbox, got %v (%v)", files, err)
	}

	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}

	// It has to be JSON. A multi-line message is the normal case here — the
	// picker sends the worker's context under the operator's sentence — and a
	// raw newline inside a JSON string is not valid JSON.
	var msg struct {
		From             string `json:"from"`
		Priority         string `json:"priority"`
		RelayingOperator bool   `json:"relaying_operator"`
		Msg              string `json:"msg"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("the inbox file is not valid JSON (%v):\n%s", err, raw)
	}

	if msg.From != "operator" {
		t.Errorf("from = %q, want operator — the gaffer weighs those differently", msg.From)
	}
	if msg.Priority != "steer" {
		t.Errorf("priority = %q, want steer — the picker never interrupts", msg.Priority)
	}
	if !msg.RelayingOperator {
		t.Error("relaying_operator should be true: the operator is the one typing")
	}
	if msg.Msg != body {
		t.Errorf("the message came back changed:\nwant %q\ngot  %q", body, msg.Msg)
	}
}

func TestGafferMsgRefusesWhatItCannotDeliver(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("FACTORY_INBOX_DIR", t.TempDir())

	if err := GafferMsg(root, "acme", "   "); err == nil {
		t.Error("an empty message should be refused rather than filed")
	}
	if err := GafferMsg(root, "", "something"); err == nil {
		t.Error("a machine with no factory has no gaffer to tell")
	}
	if err := GafferMsg(t.TempDir(), "acme", "something"); err == nil {
		t.Error("a root with no scripts/ should say so rather than silently succeed")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir := wd; ; {
		if IsRoot(dir) && !strings.HasSuffix(dir, "testdata") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("not running inside the factory checkout")
		}
		dir = parent
	}
}
