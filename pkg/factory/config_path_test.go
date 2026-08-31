package factory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstanceForPathUsesContainingWorkspaceAndLongestMatch(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(t.TempDir(), "work")
	nested := filepath.Join(base, "nested")
	if err := os.MkdirAll(filepath.Join(root, "factories"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(nested, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, workspace string) {
		body := "name = \"" + name + "\"\nworkspace_path = \"" + workspace + "\"\n"
		if err := os.WriteFile(filepath.Join(root, "factories", name+".toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("outer", base)
	write("inner", nested)
	got, ok := InstanceForPath(root, filepath.Join(nested, "src"))
	if !ok || got.Name != "inner" {
		t.Fatalf("resolved (%q, %v), want inner", got.Name, ok)
	}
	if _, ok := InstanceForPath(root, t.TempDir()); ok {
		t.Fatal("unrelated directory resolved to a factory")
	}
}
