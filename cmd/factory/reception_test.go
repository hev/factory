package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallReceptionHookPreservesSettingsAndIsIdempotent(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"permissions":{"allow":["Read"]},"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo existing"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := installReceptionHook(workspace); err != nil {
		t.Fatal(err)
	}
	if err := installReceptionHook(workspace); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["permissions"] == nil {
		t.Fatal("existing settings were lost")
	}
	hooks := settings["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(hooks) != 2 {
		t.Fatalf("SessionStart hooks = %d, want existing plus one reception hook", len(hooks))
	}
	if !hookGroupHasCommand(hooks[1], receptionHookCommand) {
		t.Fatal("reception hook was not installed")
	}
}

func TestInstallReceptionHookRefusesInvalidJSON(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{nope`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installReceptionHook(workspace); err == nil {
		t.Fatal("invalid settings were overwritten")
	}
}
