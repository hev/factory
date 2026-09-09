package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCIHomeHostAndScopeBeforeGitHub(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("FACTORY_HOSTNAME_OVERRIDE", "laptop")
	t.Setenv("PATH", t.TempDir()) // No gh: a scope/host error must precede any API read.
	if err := os.Mkdir(filepath.Join(root, "factories"), 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "factories", "demo.toml")
	if err := os.WriteFile(config, []byte("home_host = \"lab\"\nrepo_scope = [\"acme/api\"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"wait", "demo", "worker-demo-task", "outside/repo", "12"}
	if err := runCI(root, args); err == nil || !strings.Contains(err.Error(), "home_host") {
		t.Fatalf("host guard: %v", err)
	}
	t.Setenv("FACTORY_HOSTNAME_OVERRIDE", "LAB")
	if err := runCI(root, args); err == nil || !strings.Contains(err.Error(), "repo_scope") {
		t.Fatalf("scope guard: %v", err)
	}
	if err := runCI(root, []string{"poll", "../demo"}); err == nil || !strings.Contains(err.Error(), "invalid instance") {
		t.Fatalf("instance guard: %v", err)
	}
}
