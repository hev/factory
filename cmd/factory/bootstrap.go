// bootstrap.go — the first run of a binary that was installed rather than
// built.
//
// `brew install hev/tap/factory` puts one file on the machine. That is enough
// to draw the picker and nothing else: a factory is its contracts, and the
// gaffer reads them by path, mid-iteration, out of a checkout. So the first run
// of an installed binary clones one.
//
// The checkout goes to ~/workspace/factory, which is what the README has always
// said to type. It is deliberately not a dotdir: contracts are the operator's
// to read and edit, changing how a factory operates is a commit rather than a
// setting, and neither of those is true of a directory that looks like tool
// internals.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hev/factory/pkg/factory"
)

const (
	// Overridable for a fork, and for the test that must not clone anything.
	repoEnv     = "FACTORY_REPO"
	checkoutEnv = "FACTORY_CHECKOUT"

	defaultRepo = "https://github.com/hev/factory"
)

// checkoutTarget is where a cloned checkout goes.
func checkoutTarget() string {
	if target := strings.TrimSpace(os.Getenv(checkoutEnv)); target != "" {
		return target
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "workspace", "factory")
}

// ensureRoot returns the checkout this binary should read, cloning one if the
// machine has none. `root` is what factory.Root() already found: non-empty on
// every run but the first, and on every run of an in-repo build.
//
// Errors here are the operator's to act on, so each one says what to type.
func ensureRoot(root string, in io.Reader, out io.Writer) (string, string, error) {
	if root != "" {
		return root, "", nil
	}

	target := checkoutTarget()
	if target == "" {
		return "", "", fmt.Errorf("no home directory, so there is nowhere to put a checkout — set %s", checkoutEnv)
	}

	// Already cloned, never booted. `./factory` records the pointer on boot, so
	// a checkout somebody made by hand is invisible to an installed binary
	// until then. Adopt it rather than cloning a second copy.
	if factory.IsRoot(target) {
		if err := factory.RecordRoot(target); err != nil {
			return "", "", err
		}
		fmt.Fprintf(out, "factory: using the checkout at %s\n", target)
		return target, "", nil
	}

	if _, err := os.Stat(target); err == nil {
		return "", "", fmt.Errorf("%s exists and is not a factory checkout — move it, or set %s to somewhere else", target, checkoutEnv)
	}

	repo := strings.TrimSpace(os.Getenv(repoEnv))
	if repo == "" {
		repo = defaultRepo
	}

	// Asked, not assumed. Cloning a repo into somebody's home directory is not
	// a thing to do quietly on the strength of them typing one word.
	fmt.Fprintf(out, "factory: no checkout on this machine. The contracts a factory runs on\n")
	fmt.Fprintf(out, "         live in one, and it is yours to edit.\n\n")
	fmt.Fprintf(out, "         clone %s\n", repo)
	fmt.Fprintf(out, "            to %s ? [Y/n] ", target)

	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && answer == "" {
		// No terminal to ask at: launchd, cron, a pipe. Say what to type.
		return "", "", fmt.Errorf("no checkout, and nothing to ask at — run: git clone %s %s", repo, target)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "y", "yes":
	default:
		return "", "", fmt.Errorf("no checkout — run `git clone %s %s` when you are ready", repo, target)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", "", err
	}

	fmt.Fprintf(out, "\nfactory: cloning into %s\n", target)
	clone := exec.Command("git", "clone", "--quiet", repo, target)
	clone.Stdout, clone.Stderr = out, out
	if err := clone.Run(); err != nil {
		return "", "", fmt.Errorf("could not clone %s: %w", repo, err)
	}
	if !factory.IsRoot(target) {
		return "", "", fmt.Errorf("cloned %s but it has no factories/ directory — wrong repo?", repo)
	}

	// A cloned checkout cannot build its own picker: `./factory` shells out to
	// `go build`, and somebody who installed this from Homebrew has no Go
	// toolchain and no reason to. Seed it with a copy of the binary already
	// running. Root()'s second rule — two levels up from the executable — then
	// makes that copy self-locating, so the checkout works standalone.
	if err := seedPicker(target); err != nil {
		fmt.Fprintf(out, "factory: %v\n", err)
		fmt.Fprintf(out, "factory: `./factory` in the checkout will build its own with Go\n")
	}

	if err := factory.RecordRoot(target); err != nil {
		return "", "", err
	}
	return target, target, nil
}

// seedPicker copies the running binary to <root>/bin/picker, which is where
// `./factory` looks before it reaches for the compiler.
func seedPicker(root string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not find this binary to copy into the checkout: %w", err)
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return fmt.Errorf("could not resolve this binary: %w", err)
	}

	src, err := os.Open(exe)
	if err != nil {
		return err
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		return err
	}
	// Written beside the target and moved into place: the same rule `./factory`
	// follows, so nothing ever observes a half-written binary.
	tmp := filepath.Join(root, "bin", "picker.new")
	dst, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(tmp)
		return err
	}
	if err := dst.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, filepath.Join(root, "bin", "picker"))
}

// bootFreshCheckout hands over to the checkout's own `factory` script, which is
// the boot sequence this binary is not: reception on duty, every gaffer that
// belongs on this host, then the picker.
//
// Only after a clone. On every later run the checkout is found, `factory` means
// the picker exactly as it always has, and launchd is what keeps the sessions
// up.
func bootFreshCheckout(root string, out io.Writer) error {
	script := filepath.Join(root, "factory")
	if _, err := os.Stat(script); err != nil {
		fmt.Fprintf(out, "factory: cloned to %s — run ./factory there to start\n", root)
		return nil
	}

	fmt.Fprintf(out, "\nfactory: starting from %s\n\n", root)
	boot := exec.Command(script)
	boot.Dir = root
	boot.Stdin, boot.Stdout, boot.Stderr = os.Stdin, os.Stdout, os.Stderr
	// The script keeps a copy of its build on $PATH for the convenience of
	// people working in the repo. This binary came from a package manager that
	// owns its own copy, and two `factory` commands racing to be the one on
	// PATH is a bug nobody would think to look for.
	boot.Env = append(os.Environ(), "FACTORY_NO_GLOBAL_INSTALL=1")
	return boot.Run()
}
