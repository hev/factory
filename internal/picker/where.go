package picker

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Where a worker is working. The ledger says which repo it was dispatched
// against; the pane says which directory it is actually sitting in, and the
// two disagree often enough to be worth showing together — a worker in the
// wrong worktree looks exactly like a worker in the right one from a row of
// columns.
type Where struct {
	Root     string // the repo or worktree root above the pane's directory
	Branch   string // the checked-out branch, or "detached at <sha>"
	Worktree bool   // a linked worktree rather than the repo's own checkout
}

// branch is read out of .git rather than out of `git`. The picker re-reads the
// floor every two seconds and this runs once per row, so a subprocess per row
// per refresh is a cost the screen would eventually be able to feel — and
// every answer wanted here is one file read away.
//
// A directory that is not in a repo at all is the normal case for a shell, and
// it is an empty Where rather than an error.
func whereOf(dir string) Where {
	if dir == "" {
		return Where{}
	}
	if cached, ok := whereCache.get(dir); ok {
		return cached
	}
	found := resolveWhere(dir)
	whereCache.put(dir, found)
	return found
}

func resolveWhere(dir string) Where {
	root, gitDir, linked := gitDirFor(dir)
	if gitDir == "" {
		return Where{}
	}
	return Where{Root: root, Branch: headBranch(gitDir), Worktree: linked}
}

// gitDirFor walks up from dir to the first .git, and returns the directory
// holding it, the git directory itself, and whether it is a linked worktree.
//
// In a worktree, .git is a file reading `gitdir: /repo/.git/worktrees/<name>`,
// and that directory has its own HEAD — which is the whole reason a worktree
// can be on a different branch from the checkout it was made from.
func gitDirFor(dir string) (root, gitDir string, linked bool) {
	for cur := filepath.Clean(dir); ; {
		candidate := filepath.Join(cur, ".git")
		if info, err := os.Stat(candidate); err == nil {
			if info.IsDir() {
				return cur, candidate, false
			}
			raw, err := os.ReadFile(candidate)
			if err != nil {
				return cur, "", false
			}
			pointer := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), "gitdir:"))
			if pointer == "" {
				return cur, "", false
			}
			if !filepath.IsAbs(pointer) {
				pointer = filepath.Join(cur, pointer)
			}
			return cur, pointer, true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", "", false
		}
		cur = parent
	}
}

// headBranch reads the branch name out of a git directory's HEAD. A detached
// HEAD holds a bare sha instead of a ref, and saying so is more use than
// printing forty hex characters into a fourteen-column field.
func headBranch(gitDir string) string {
	raw, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(raw))
	if ref := strings.TrimPrefix(head, "ref: "); ref != head {
		return strings.TrimPrefix(strings.TrimSpace(ref), "refs/heads/")
	}
	if len(head) > 7 {
		head = head[:7]
	}
	if head == "" {
		return ""
	}
	return "detached at " + head
}

// ── the cache ────────────────────────────────────────────────

// whereTTL is how long a resolved location is reused. A worker changes branch
// at most a handful of times over its life and never between two frames, so
// re-reading on every refresh buys nothing; a few seconds is short enough that
// a rebase shows up while somebody is still looking at the row.
const whereTTL = 5 * time.Second

var whereCache = &locations{at: map[string]locationEntry{}}

type locationEntry struct {
	where Where
	read  time.Time
}

// locations is read from the refresh goroutines that fan out over the rows, so
// it carries its own lock rather than relying on the caller's.
type locations struct {
	mu sync.Mutex
	at map[string]locationEntry
}

func (l *locations) get(dir string) (Where, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.at[dir]
	if !ok || time.Since(entry.read) > whereTTL {
		return Where{}, false
	}
	return entry.where, true
}

func (l *locations) put(dir string, where Where) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.at[dir] = locationEntry{where: where, read: time.Now()}
}

// homePath shortens a path the way a person writes one. The panel spends its
// width on the part of a path that differs between workers, and forty
// characters of home directory is not it.
func homePath(path string) string {
	if path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest := strings.TrimPrefix(path, home+string(filepath.Separator)); rest != path {
		return "~" + string(filepath.Separator) + rest
	}
	return path
}
