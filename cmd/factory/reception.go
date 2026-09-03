package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hev/factory/pkg/factory"
)

// Claude Code surfaces non-zero SessionStart hooks as hook errors. whoami's
// non-zero result is useful at a shell but an unrelated workspace is ordinary,
// so the hook deliberately turns that result into silent success.
const (
	receptionHookCommand       = `root="$(cat "$HOME/.factory/root")"; "$root/factory" whoami 2>/dev/null || true`
	legacyReceptionHookCommand = "factory whoami 2>/dev/null || true"
)

// runWhoami is both a useful shell query and the SessionStart hook's prompt.
// It stays silent when the cwd is unrelated so ordinary Claude sessions remain
// ordinary.
func runWhoami(root string, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: factory whoami")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	inst, ok := factory.InstanceForPath(root, cwd)
	if !ok {
		return fmt.Errorf("%s does not belong to a configured factory", cwd)
	}

	// The desk may be opened anywhere the workspace is checked out; the
	// factory only runs on its home_host. Ask that machine rather than
	// describing this one. See reception_remote.go.
	host, away := awayHost(inst)
	state := factory.GafferState(inst)
	if away {
		state = gafferStateOn(host, inst)
		fmt.Printf("Factory: %s\nGaffer: %s (on %s)\n", inst.Name, state, host)
	} else {
		fmt.Printf("Factory: %s\nGaffer: %s\n", inst.Name, state)
	}
	// unknown is not down: the home host did not answer, so nothing below can
	// be reported honestly and the desk is not offered.
	if state == "down" || state == stateUnknown {
		return nil
	}
	door := fmt.Sprintf("GitHub pull requests in %s targeting %s", inst.PlansRepo, inst.Branch())
	if inst.LinearTeam != "" {
		door = fmt.Sprintf("Linear team %s; approved state %s", inst.LinearTeam, inst.LinearApprovedState)
	}
	fmt.Printf("Plans door: %s\n", door)
	events, waiting, hasWaiting := unreadEvents(root, inst.Name), 0, false
	if away {
		events = unreadEventsOn(host, inst.Name)
		waiting, hasWaiting = waitingOn(host, inst.Name)
	} else {
		waiting, hasWaiting = latestWaiting(inst.Name)
	}
	if events >= 0 {
		fmt.Printf("Waiting event lines: %d\n", events)
	}
	if hasWaiting {
		fmt.Printf("Waiting on operator at last beat: %d\n", waiting)
	}
	fmt.Println("Load the reception skill now and act as this factory's front desk.")
	return nil
}

func latestWaiting(instance string) (int, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, false
	}
	f, err := os.Open(filepath.Join(home, ".factory", "beats", instance+".jsonl"))
	if err != nil {
		return 0, false
	}
	defer f.Close()
	last := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			last = scanner.Text()
		}
	}
	if last == "" || scanner.Err() != nil {
		return 0, false
	}
	var beat struct {
		Waiting int `json:"waiting"`
	}
	if err := json.Unmarshal([]byte(last), &beat); err != nil {
		return 0, false
	}
	return beat.Waiting, true
}

func unreadEvents(root, instance string) int {
	cmd := exec.Command(filepath.Join(root, "scripts", "factory-events.sh"), instance, "--count", "--reader", "reception")
	out, err := cmd.Output()
	if err != nil {
		return -1
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n); err != nil {
		return -1
	}
	return n
}

func runAdopt(root string, args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: factory adopt <instance>")
	}
	for _, inst := range factory.LoadInstances(root) {
		if inst.Name == args[0] {
			if err := installReceptionHook(inst.Workspace()); err != nil {
				return err
			}
			fmt.Printf("installed reception hook in %s\n", inst.Workspace())
			return nil
		}
	}
	return fmt.Errorf("no factory named %q — see factory list", args[0])
}

func installReceptionHook(workspace string) error {
	if workspace == "" {
		return fmt.Errorf("factory has no workspace_path")
	}
	dir := filepath.Join(workspace, ".claude")
	path := filepath.Join(dir, "settings.json")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	settings := map[string]any{}
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) != 0 {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("%s is not valid JSON: %w", path, err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		settings["hooks"] = hooks
	}
	starts, _ := hooks["SessionStart"].([]any)
	found := false
	for _, group := range starts {
		if hookGroupHasCommand(group, receptionHookCommand) {
			return nil
		}
		if replaceHookCommand(group, legacyReceptionHookCommand, receptionHookCommand) {
			found = true
		}
	}
	if !found {
		starts = append(starts, map[string]any{"hooks": []any{map[string]any{
			"type": "command", "command": receptionHookCommand,
		}}})
	}
	hooks["SessionStart"] = starts
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}

func replaceHookCommand(value any, old, command string) bool {
	group, _ := value.(map[string]any)
	hooks, _ := group["hooks"].([]any)
	for _, value := range hooks {
		hook, _ := value.(map[string]any)
		if hook["type"] == "command" && hook["command"] == old {
			hook["command"] = command
			return true
		}
	}
	return false
}

func hookGroupHasCommand(value any, command string) bool {
	group, _ := value.(map[string]any)
	hooks, _ := group["hooks"].([]any)
	for _, value := range hooks {
		hook, _ := value.(map[string]any)
		if hook["type"] == "command" && hook["command"] == command {
			return true
		}
	}
	return false
}
