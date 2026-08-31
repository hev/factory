package picker

import (
	"os"
	"path/filepath"
	"testing"
)

// The branch is read out of .git rather than out of `git`, so the parsing is
// this package's problem and the shapes it has to survive are worth pinning.
func TestBranchFromARepo(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(gitDir, "HEAD"), "ref: refs/heads/index-rebuild\n")

	// A directory below the root still resolves: a pane is almost never
	// sitting at the top of the checkout.
	deep := filepath.Join(dir, "src", "index")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveWhere(deep)
	if got.Branch != "index-rebuild" {
		t.Errorf("branch = %q, want index-rebuild", got.Branch)
	}
	if got.Root != dir {
		t.Errorf("root = %q, want %q", got.Root, dir)
	}
	if got.Worktree {
		t.Error("a repo's own checkout is not a linked worktree")
	}
}

// A worktree's .git is a file pointing at the real git directory, and that
// directory has its own HEAD — which is the whole reason two workers of one
// factory can be on different branches of the same repo.
func TestBranchFromALinkedWorktree(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "api")
	linked := filepath.Join(base, "api-index")
	worktreeGit := filepath.Join(repo, ".git", "worktrees", "api-index")
	for _, dir := range []string{worktreeGit, linked} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	write(t, filepath.Join(worktreeGit, "HEAD"), "ref: refs/heads/index-rebuild\n")
	write(t, filepath.Join(linked, ".git"), "gitdir: "+worktreeGit+"\n")

	got := resolveWhere(linked)
	if got.Branch != "index-rebuild" {
		t.Errorf("branch = %q, want the worktree's own branch index-rebuild", got.Branch)
	}
	if !got.Worktree {
		t.Error("a linked worktree should say so — it is the difference worth showing")
	}
}

func TestDetachedHeadSaysSoRatherThanPrintingASha(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, ".git", "HEAD"), "9f2c1ab7d4e5f60718293a4b5c6d7e8f90a1b2c3\n")

	got := resolveWhere(dir).Branch
	if got != "detached at 9f2c1ab" {
		t.Errorf("detached HEAD = %q, want a short sha and the word for it", got)
	}
}

// A shell in somebody's home directory is the normal case, not an error.
func TestNoRepoIsAnEmptyWhere(t *testing.T) {
	if got := resolveWhere(t.TempDir()); got != (Where{}) {
		t.Errorf("a directory outside any repo = %+v, want an empty Where", got)
	}
	if got := whereOf(""); got != (Where{}) {
		t.Errorf("a pane with no directory = %+v, want an empty Where", got)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
