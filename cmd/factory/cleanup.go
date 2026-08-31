// factory cleanup — take a factory apart and leave nothing of it behind.
//
// The counterpart to `factory init`. Init writes one file and cleanup removes
// everything that file caused to exist: the sessions, the child worktrees, the
// state under ~/.factory, and the config itself. It is what you run when a
// factory is finished, when an experiment is over, or when you want to start
// the onboarding conversation from nothing.
//
// Three rules make it safe enough to hand somebody:
//
//  1. It says exactly what it will delete, path by path, and waits for "y".
//  2. It only ever touches an allowlist: ~/.factory, the checkout's
//     factories/<name>.toml, and the one .factory-watermark file inside the
//     instance's workspace. There is no glob and nothing recursive above those.
//  3. It never touches GitHub. Issues, pull requests, branches on the remote
//     and anything a worker actually shipped are the work, not the factory,
//     and a teardown that could delete those would be a different tool.
//
// A child worktree with uncommitted work is reported and left standing —
// removing somebody's unfinished work is not cleanup.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hev/factory/internal/tmuxctl"
	"github.com/hev/factory/internal/ui"
	"github.com/hev/factory/pkg/factory"
)

// removal is one thing the plan will delete, with the line the confirm shows.
type removal struct {
	path string
	what string
}

// worktree is a child's git worktree: registered with a repo, so it comes out
// with `git worktree remove` rather than a delete.
type worktree struct {
	path  string
	repo  string // the tree that registered it
	dirty bool   // uncommitted work — reported, never removed
}

// cleanupPlan is everything one invocation would do, worked out before
// anything happens. Building it is side-effect free, which is what makes
// --dry-run honest and the confirm accurate.
type cleanupPlan struct {
	instances []string
	all       bool
	sessions  []string
	worktrees []worktree
	paths     []removal
}

func (p cleanupPlan) empty() bool {
	return len(p.sessions) == 0 && len(p.worktrees) == 0 && len(p.paths) == 0
}

func runCleanup(root string, args []string) error {
	if root == "" {
		return fmt.Errorf("no factory checkout found — run ./factory from the repo once, or set FACTORY_ROOT")
	}

	var all, yes, dryRun bool
	var named []string
	for _, arg := range args {
		switch arg {
		case "--all":
			all = true
		case "--yes", "-y":
			yes = true
		case "--dry-run":
			dryRun = true
		case "-h", "--help":
			fmt.Print(cleanupUsage)
			return nil
		default:
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("unknown argument %q — see factory cleanup --help", arg)
			}
			named = append(named, arg)
		}
	}

	configured := map[string]bool{}
	for _, inst := range factory.LoadInstances(root) {
		configured[inst.Name] = true
	}

	switch {
	case all && len(named) > 0:
		return fmt.Errorf("--all takes no instance name: it is every factory on this machine")
	case !all && len(named) == 0:
		return fmt.Errorf("name the factory to remove, or pass --all — see factory cleanup --help")
	}

	if !all {
		for _, name := range named {
			if !configured[name] {
				return fmt.Errorf("no factory called %q — configured: %s", name, strings.Join(sortedKeys(configured), ", "))
			}
		}
	}

	plan := buildCleanupPlan(root, named, all)
	if plan.empty() {
		fmt.Println("  " + ui.Dim.Render("Nothing left to clean up."))
		return nil
	}

	printCleanupPlan(plan)

	if dryRun {
		fmt.Println("\n  " + ui.Dim.Render("Dry run — nothing removed."))
		return nil
	}
	if !yes {
		fmt.Print("\n  " + ui.Alarm.Render("Delete all of it? [y/N] "))
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
		default:
			fmt.Println("  " + ui.Dim.Render("Cancelled. Nothing was removed."))
			return nil
		}
	}

	execCleanup(plan)
	return nil
}

// buildCleanupPlan decides what would go, and touches nothing.
func buildCleanupPlan(root string, named []string, all bool) cleanupPlan {
	plan := cleanupPlan{all: all}
	instances := named
	if all {
		for _, inst := range factory.LoadInstances(root) {
			instances = append(instances, inst.Name)
		}
	}
	sort.Strings(instances)
	plan.instances = instances

	home, _ := os.UserHomeDir()
	dot := filepath.Join(home, ".factory")

	// Sessions, from what is actually running rather than from what the naming
	// convention predicts.
	wanted := map[string]bool{}
	for _, name := range instances {
		wanted[factory.GafferFor(name)] = true
	}
	fleet := factory.NewScope(root)
	now := time.Now()
	for _, session := range tmuxctl.ListSessions() {
		member := fleet.Classify(session.Name, now)
		switch {
		case member.Kind == factory.NotFactory:
			continue
		case all:
		case wanted[session.Name]:
		case member.Kind == factory.Worker && contains(instances, member.Instance):
		default:
			continue
		}
		plan.sessions = append(plan.sessions, session.Name)
	}
	sort.Strings(plan.sessions)

	// Per-instance state. Each of these is a file or a directory named for the
	// instance, so the allowlist is literal: no globbing, nothing above ~/.factory.
	for _, name := range instances {
		for _, item := range []struct{ path, what string }{
			{filepath.Join(root, "factories", name+".toml"), "the config"},
			{filepath.Join(dot, "heartbeat", name), "liveness heartbeat"},
			{filepath.Join(dot, "beats", name+".jsonl"), "beat telemetry"},
			{filepath.Join(dot, "briefs", name), "worker briefs"},
			{filepath.Join(dot, "harvest", name), "harvested worker panes"},
			{filepath.Join(dot, "inbox", name), "reception inbox"},
			{filepath.Join(dot, "iterations", name), "one-shot iteration state"},
			{filepath.Join(dot, "reception", name), "its front desk's memory"},
		} {
			if exists(item.path) {
				plan.paths = append(plan.paths, removal{item.path, item.what})
			}
		}

		// Child ledger entries are keyed by session name, not by instance, so
		// they are matched rather than named.
		for _, entry := range readDirNames(filepath.Join(dot, "children")) {
			if strings.HasPrefix(entry, factory.WorkerPrefix(name)) {
				plan.paths = append(plan.paths, removal{
					filepath.Join(dot, "children", entry), "child ledger entry"})
			}
		}

		plan.worktrees = append(plan.worktrees, worktreesFor(dot, name)...)

		// The one thing outside ~/.factory and the checkout: a single
		// untracked file in the workspace the gaffer ran in.
		if mark := watermarkFor(root, name); mark != "" && exists(mark) {
			plan.paths = append(plan.paths, removal{mark, "the plan watermark (untracked, in your repo)"})
		}
	}

	if all {
		// The bootstrap desk belongs to no instance, and whatever is left in
		// these directories belonged to a factory that is already gone.
		for _, item := range []struct{ path, what string }{
			{filepath.Join(dot, "reception"), "the bootstrap desk's memory"},
			{filepath.Join(dot, "heartbeat"), "heartbeats"},
			{filepath.Join(dot, "beats"), "beat telemetry"},
			{filepath.Join(dot, "children"), "child ledger"},
			{filepath.Join(dot, "harvest"), "harvested worker panes"},
			{filepath.Join(dot, "briefs"), "worker briefs"},
			{filepath.Join(dot, "inbox"), "reception inboxes"},
			{filepath.Join(dot, "iterations"), "one-shot iteration state"},
			{filepath.Join(dot, "worktrees"), "child worktree roots"},
			{filepath.Join(dot, "summaries"), "cached pane labels"},
		} {
			if exists(item.path) {
				plan.paths = append(plan.paths, removal{item.path, item.what})
			}
		}
	}

	// A directory and something inside it can both be planned — --all names
	// ~/.factory/reception while an instance named its own desk under it. The
	// parent wins, so the confirm lists each thing once and reads honestly.
	plan.paths = dropCoveredPaths(plan.paths)
	return plan
}

// dropCoveredPaths removes any entry that lives inside another entry, keeping
// the order the plan was built in.
func dropCoveredPaths(paths []removal) []removal {
	out := make([]removal, 0, len(paths))
	for i, item := range paths {
		covered := false
		for j, other := range paths {
			if i == j {
				continue
			}
			// An exact duplicate is dropped once, by the later of the two.
			if item.path == other.path && j < i {
				covered = true
				break
			}
			if strings.HasPrefix(item.path, other.path+string(filepath.Separator)) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, item)
		}
	}
	return out
}

// worktreesFor finds a factory's child worktrees and asks each one whether it
// has uncommitted work. A dirty tree is planned but flagged, and the executor
// leaves it standing.
func worktreesFor(dot, instance string) []worktree {
	base := filepath.Join(dot, "worktrees", instance)
	var out []worktree
	for _, name := range readDirNames(base) {
		path := filepath.Join(base, name)
		if !isDir(path) {
			continue
		}
		tree := worktree{path: path, repo: gitCommon(path)}
		status, err := gitOutput(path, "status", "--porcelain")
		tree.dirty = err != nil || strings.TrimSpace(status) != ""
		out = append(out, tree)
	}
	return out
}

// gitCommon resolves the repo a worktree was created from, which is where
// `git worktree remove` has to run.
func gitCommon(path string) string {
	common, err := gitOutput(path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return ""
	}
	// <repo>/.git -> <repo>
	return filepath.Dir(strings.TrimSpace(common))
}

// watermarkFor is the instance's workspace_path plus .factory-watermark. It is
// read from the config rather than guessed, and returns "" when the config has
// no workspace or the path does not resolve.
func watermarkFor(root, instance string) string {
	data, err := os.ReadFile(filepath.Join(root, "factories", instance+".toml"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "workspace_path" {
			continue
		}
		value = strings.TrimSpace(value)
		if hash := strings.Index(value, "#"); hash >= 0 {
			value = strings.TrimSpace(value[:hash])
		}
		value = strings.Trim(value, `"`)
		if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(value, "~/") {
			value = filepath.Join(home, value[2:])
		}
		if value == "" {
			return ""
		}
		return filepath.Join(value, ".factory-watermark")
	}
	return ""
}

func printCleanupPlan(plan cleanupPlan) {
	what := strings.Join(plan.instances, ", ")
	if plan.all {
		what = "every factory on this machine"
		if len(plan.instances) > 0 {
			what += " (" + strings.Join(plan.instances, ", ") + ")"
		}
	}

	fmt.Println()
	fmt.Println("  " + ui.Alarm.Render("🧹 CLEAN UP "+what))

	if len(plan.sessions) > 0 {
		fmt.Println("  " + ui.Header.Render(fmt.Sprintf("%d session(s) — agents get TERM, then the session goes:", len(plan.sessions))))
		for _, session := range plan.sessions {
			fmt.Println("    " + session)
		}
	}
	if len(plan.worktrees) > 0 {
		fmt.Println("  " + ui.Header.Render("child worktrees:"))
		for _, tree := range plan.worktrees {
			note := "git worktree remove"
			if tree.dirty {
				note = ui.Alarm.Render("uncommitted work — kept")
			}
			fmt.Printf("    %s  %s\n", tilde(tree.path), ui.Dim.Render(note))
		}
	}
	if len(plan.paths) > 0 {
		fmt.Println("  " + ui.Header.Render(fmt.Sprintf("%d path(s):", len(plan.paths))))
		for _, item := range plan.paths {
			fmt.Printf("    %-52s %s\n", tilde(item.path), ui.Dim.Render(item.what))
		}
	}
	fmt.Println("  " + ui.Dim.Render("GitHub is not touched: issues, pull requests and remote branches stay."))
}

func execCleanup(plan cleanupPlan) {
	var killed, removed, kept int

	for _, session := range plan.sessions {
		_ = tmuxctl.KillSession(session)
		if !tmuxctl.HasSession(session) {
			killed++
		}
	}

	for _, tree := range plan.worktrees {
		if tree.dirty {
			kept++
			fmt.Printf("  kept %s — uncommitted work\n", tilde(tree.path))
			continue
		}
		if tree.repo != "" {
			if _, err := gitOutput(tree.repo, "worktree", "remove", tree.path); err != nil {
				fmt.Printf("  could not remove worktree %s: %v\n", tilde(tree.path), err)
				kept++
				continue
			}
			_, _ = gitOutput(tree.repo, "worktree", "prune")
		} else if err := os.RemoveAll(tree.path); err != nil {
			fmt.Printf("  could not remove %s: %v\n", tilde(tree.path), err)
			kept++
			continue
		}
		removed++
	}

	for _, item := range plan.paths {
		if err := os.RemoveAll(item.path); err != nil {
			fmt.Printf("  could not remove %s: %v\n", tilde(item.path), err)
			continue
		}
		removed++
	}

	summary := fmt.Sprintf("✓ %d session(s) stopped, %d path(s) removed", killed, removed)
	if kept > 0 {
		summary += fmt.Sprintf(", %d kept", kept)
	}
	fmt.Println("  " + ui.Flash.Render(summary))
	fmt.Println("  " + ui.Dim.Render("./factory now starts the onboarding conversation from nothing."))
}

// ── small helpers ─────────────────────────────────────────────

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func readDirNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	sort.Strings(out)
	return out
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

func tilde(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || !strings.HasPrefix(path, home) {
		return path
	}
	return "~" + strings.TrimPrefix(path, home)
}

const cleanupUsage = `factory cleanup — remove a factory and everything it left behind

  factory cleanup acme          that factory: sessions, state, config
  factory cleanup --all         every factory on this machine
  factory cleanup acme --dry-run   print what would go, remove nothing
  factory cleanup acme --yes       skip the confirm (scripts, teardown runs)

It removes the factory's tmux sessions (TERM to the agents first), its child
worktrees, its state under ~/.factory, its factories/<name>.toml, and the
.factory-watermark in its workspace.

It does not touch GitHub, and it does not touch a child worktree with
uncommitted work — that one is reported and left standing.
`
