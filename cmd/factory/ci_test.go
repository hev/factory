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

func TestCIPollWakesDispatchOnlyInEventModeAndReportsWakeFailure(t *testing.T) {
	root, home, bin := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	t.Setenv("FACTORY_HOSTNAME_OVERRIDE", "fixture")
	if err := os.Mkdir(filepath.Join(root, "factories"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "factories", "demo.toml"), []byte("home_host=\"fixture\"\nrepo_scope=[\"acme/app\"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// With no watches, the poll performs no network requests. A missing Python
	// must not affect legacy mode.
	if err := runCI(root, []string{"poll", "demo"}); err != nil {
		t.Fatal(err)
	}
	controller := filepath.Join(home, ".factory", "controller")
	if err := os.MkdirAll(controller, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(controller, "enabled"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(bin, "python3")
	// Check the actual CLI argument and exercise the surfaced failure path.
	if err := os.WriteFile(python, []byte("#!/bin/sh\n[ \"$2\" = wake ] || exit 8\necho fixture-wake-failed\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	err := runCI(root, []string{"poll", "demo"})
	if err == nil || !strings.Contains(err.Error(), "fixture-wake-failed") {
		t.Fatalf("wake failure was not reported: %v", err)
	}
	if err := os.WriteFile(python, []byte("#!/bin/sh\n[ \"$2\" = wake ]\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := runCI(root, []string{"poll", "demo"}); err != nil {
		t.Fatal(err)
	}
}
