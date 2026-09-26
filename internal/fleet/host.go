package fleet

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hev/factory/internal/tmuxctl"
)

// Everything in this file runs on the host that owns the session. The laptop
// reaches it through Handle, over ssh for every host but itself.

// StartRequest is what `factory run` asks a host for.
type StartRequest struct {
	Repo    string `json:"repo"` // OWNER/REPO
	Task    string `json:"task"`
	Harness string `json:"harness"`
	Model   string `json:"model,omitempty"`
	Base    string `json:"base,omitempty"` // branch to cut from; the repo's default when empty
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Start makes the worktree, writes the brief and starts the runner.
func Start(req StartRequest) (Meta, error) {
	if !repoPattern.MatchString(req.Repo) {
		return Meta{}, fmt.Errorf("repo must be OWNER/REPO, got %q", req.Repo)
	}
	if strings.TrimSpace(req.Task) == "" {
		return Meta{}, errors.New("no task")
	}
	if req.Harness == "" {
		req.Harness = "claude"
	}
	if _, err := exec.LookPath(req.Harness); err != nil {
		return Meta{}, fmt.Errorf("%s is not installed on %s", req.Harness, hostname())
	}

	clone, base, unlock, err := freshClone(req.Repo, req.Base)
	if err != nil {
		return Meta{}, err
	}
	defer unlock() // held through worktree add: a fan-out starts many at once
	id := NewID()
	dir := sessionDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Meta{}, err
	}
	m := Meta{
		ID:        id,
		Host:      hostname(),
		Repo:      req.Repo,
		Base:      base,
		Branch:    "factory/" + id,
		Worktree:  filepath.Join(dir, "work"),
		Harness:   req.Harness,
		Model:     req.Model,
		Task:      req.Task,
		Login:     ghLogin(),
		CreatedAt: time.Now().UTC(),
	}
	if out, err := git(clone, "worktree", "add", "--quiet", "-b", m.Branch, m.Worktree, "origin/"+base); err != nil {
		os.RemoveAll(dir)
		return Meta{}, fmt.Errorf("worktree: %s", out)
	}
	author, _ := git(m.Worktree, "config", "user.name")
	if err := writeJSON(filepath.Join(dir, "meta.json"), m); err != nil {
		return Meta{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte(brief(m, author)), 0o644); err != nil {
		return Meta{}, err
	}
	if err := saveState(id, State{Status: Running}); err != nil {
		return Meta{}, err
	}
	return m, startRunner(m)
}

// brief is the only thing the factory adds to a task: where the session is,
// who it acts as, and the two rules every earlier session that broke them
// paid for.
func brief(m Meta, author string) string {
	who := m.Login
	if who == "" {
		who = "this host's gh login"
	}
	if author != "" {
		who += " (git author " + author + ")"
	}
	return fmt.Sprintf(`You are a background coding session started by hev factory on %s, acting as %s.
Nobody is watching you live. Whoever started you reads your pull request and this transcript, and may send follow-ups.

Your worktree is %s, on branch %s, cut from %s of %s.
Push that branch and open a pull request from it. Open it as a draft as soon as you have a first commit, so the work is visible early; mark it ready when it is.
Never end a turn waiting on a background task: wait for it in the foreground, or finish without it.
Finish with a short summary: what changed, the pull request link, and anything left open.

Task:
%s
`, m.Host, who, m.Worktree, m.Branch, m.Base, m.Repo, m.Task)
}

// freshClone is the host's own clone of a repo, fetched now. The factory
// never works in a person's checkout: that one has their branch and their
// uncommitted work in it.
// It returns holding the clone's lock, for the caller to release once its
// worktree exists.
func freshClone(repo, base string) (clone, _ string, unlock func(), err error) {
	clone = filepath.Join(Home(), "repos", repo)
	if err := os.MkdirAll(filepath.Dir(clone), 0o755); err != nil {
		return "", "", nil, err
	}
	if unlock, err = flock(clone+".lock", true); err != nil {
		return "", "", nil, err
	}
	defer func() {
		if err != nil {
			unlock()
		}
	}()
	if _, err := os.Stat(filepath.Join(clone, ".git")); err != nil {
		cmd := exec.Command("gh", "repo", "clone", repo, clone, "--", "--quiet")
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", "", nil, fmt.Errorf("clone %s: %s", repo, strings.TrimSpace(string(out)))
		}
	} else if out, err := git(clone, "fetch", "--quiet", "--prune", "origin"); err != nil {
		return "", "", nil, fmt.Errorf("fetch %s: %s", repo, out)
	}
	if base == "" {
		head, err := git(clone, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
		if err != nil {
			git(clone, "remote", "set-head", "origin", "--auto")
			head, err = git(clone, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
		}
		if err != nil {
			return "", "", nil, fmt.Errorf("default branch of %s: %s", repo, head)
		}
		base = strings.TrimPrefix(head, "origin/")
	}
	return clone, base, unlock, nil
}

func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// startRunner starts `factory _runner` in a tmux session of its own, under a
// login shell so the harness sees the same PATH and secrets a person would.
// Starting one while another is alive is harmless: the second finds the
// runner lock taken and exits.
func startRunner(m Meta) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return newTmux(runnerName(m.ID), m, "exec "+shellQuote(exe)+" _runner "+m.ID)
}

func newTmux(name string, m Meta, command string) error {
	cmd := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", m.Worktree, "-x", "200", "-y", "50",
		"-e", "FACTORY_SESSION="+m.ID, "-e", "FACTORY_HOME="+Home(), loginShell(), "-lc", command)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// tmuxSessions is every tmux session belonging to a session: its runners and
// its TUI.
func tmuxSessions(id string) []string {
	var out []string
	for _, s := range tmuxctl.ListSessions() {
		if s.Name == TmuxName(id) || strings.HasPrefix(s.Name, TmuxName(id)+"-") {
			out = append(out, s.Name)
		}
	}
	return out
}

func stopRunners(id string) {
	for _, name := range tmuxSessions(id) {
		if name != TmuxName(id) {
			tmuxctl.KillSession(name)
		}
	}
	for i := 0; i < 50 && runnerAlive(id); i++ {
		time.Sleep(100 * time.Millisecond)
	}
}

func loginShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/zsh"
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// RunRunner drives a session's turns until its inbox is empty: the brief
// first, then each follow-up as a resumed turn. It is what runs inside the
// tmux session, and it exits when there is nothing left to say. Its pane
// closes with it, so an error it cannot recover from is written into the
// session's record, where ls and peek will show it.
func RunRunner(id string) error {
	err := runTurns(id)
	if err != nil {
		if f, ferr := os.OpenFile(filepath.Join(sessionDir(id), "stderr.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); ferr == nil {
			fmt.Fprintln(f, "runner: "+err.Error())
			f.Close()
		}
		updateState(id, func(s *State) { s.Status, s.Result = Failed, "runner: "+err.Error() })
	}
	return err
}

func runTurns(id string) error {
	lockPath := filepath.Join(sessionDir(id), "runner.lock")
	unlock, err := flock(lockPath, false)
	if err != nil {
		if errors.Is(err, errLocked) {
			return nil // another runner has it
		}
		return err
	}
	defer func() { unlock() }()
	m, err := loadMeta(id)
	if err != nil {
		return err
	}
	for {
		st := loadState(id)
		if st.Status == Killed {
			return nil
		}
		var input string
		var consumed []string
		if st.Turns == 0 && st.HarnessSession == "" {
			data, err := os.ReadFile(filepath.Join(sessionDir(id), "prompt.md"))
			if err != nil {
				return err
			}
			input = string(data)
		} else if consumed = inbox(id); len(consumed) == 0 && st.Turns == 0 {
			// The first turn started and never finished: the host went down,
			// or the runner was killed under it.
			input = "You were interrupted before your first turn finished. Carry on with the task."
		} else {
			if len(consumed) == 0 {
				// A follow-up can land between the look and the exit. Let go,
				// look once more, and take the lock back if one did: `send`
				// starts a runner only when the lock is free, so between the
				// two of us nothing is stranded.
				unlock()
				unlock = func() {}
				if len(inbox(id)) == 0 {
					return nil
				}
				if unlock, err = flock(lockPath, false); err != nil {
					unlock = func() {}
					return nil
				}
				continue
			}
			var parts []string
			for _, f := range consumed {
				data, _ := os.ReadFile(f)
				parts = append(parts, strings.TrimSpace(string(data)))
			}
			input = strings.Join(parts, "\n\n")
			if st.HarnessSession == "" {
				updateState(id, func(s *State) {
					s.Status, s.Result = Failed, "no harness session to resume; the first turn never started"
				})
				return nil
			}
		}
		turn := st.Turns + 1
		updateState(id, func(s *State) { s.Status = Running })
		marker, _ := json.Marshal(map[string]any{"type": "factory", "turn": turn, "input": input, "at": time.Now().UTC()})
		appendLog(id, marker)
		for _, f := range consumed {
			os.Remove(f)
		}
		fmt.Printf("\n▶ turn %d\n", turn)
		code := runTurn(m, st.HarnessSession, input)
		updateState(id, func(s *State) {
			s.Turns = turn
			if s.Status == Killed {
				return
			}
			if code != 0 {
				s.Exit = code
			}
			s.Status = Done
			if s.Exit != 0 {
				s.Status = Failed
			}
		})
	}
}

// runTurn runs one harness invocation, appending its stream to the log and
// echoing a readable rendering to the pane.
func runTurn(m Meta, resume, input string) int {
	argv := turnCommand(m, resume)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = m.Worktree
	cmd.Env = append(os.Environ(), "FACTORY_SESSION="+m.ID, "FACTORY_HOST="+m.Host)
	cmd.Stdin = strings.NewReader(input)
	stderr, _ := os.OpenFile(filepath.Join(sessionDir(m.ID), "stderr.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if stderr != nil {
		defer stderr.Close()
		cmd.Stderr = io.MultiWriter(stderr, os.Stderr)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 127
	}
	updateState(m.ID, func(s *State) { s.Exit = 0 })
	if err := cmd.Start(); err != nil {
		updateState(m.ID, func(s *State) { s.Result = err.Error() })
		return 127
	}
	r := bufio.NewReaderSize(stdout, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			appendLog(m.ID, line)
			// Most lines change nothing the state records; only take the
			// lock for the few that do.
			var probe State
			if observe(line, &probe); probe != (State{}) {
				updateState(m.ID, func(s *State) { observe(line, s) })
			}
			for _, l := range Render([][]byte{line}) {
				fmt.Println(l)
			}
		}
		if err != nil {
			break
		}
	}
	if err := cmd.Wait(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() > 0 {
			return exit.ExitCode()
		}
		return 1
	}
	return 0
}

func appendLog(id string, line []byte) {
	f, err := os.OpenFile(filepath.Join(sessionDir(id), "log.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(append(line, '\n'))
}

// List is every session on this host. With prs set, it asks GitHub about
// each session whose pull request could still change.
func List(prs bool) ([]Session, error) {
	entries, err := os.ReadDir(sessionsDir())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var out []Session
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := loadMeta(e.Name())
		if err != nil {
			continue
		}
		out = append(out, Session{Meta: m, State: liveState(m.ID), Queued: len(inbox(m.ID))})
	}
	if prs {
		var wg sync.WaitGroup
		sem := make(chan struct{}, 8)
		for i := range out {
			if pr := out[i].PR; pr != nil && pr.State != "OPEN" {
				continue
			}
			wg.Add(1)
			go func(s *Session) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if pr := lookupPR(s.Repo, s.Branch); pr != nil {
					s.PR = pr
					updateState(s.ID, func(st *State) { st.PR = pr })
				}
			}(&out[i])
		}
		wg.Wait()
	}
	return out, nil
}

// liveState is the recorded state, corrected by what is actually running.
func liveState(id string) State {
	st := loadState(id)
	switch st.Status {
	case Running:
		if !runnerAlive(id) && len(tmuxSessions(id)) == 0 {
			st.Status = Died
		}
	case Interactive:
		if !tmuxctl.HasSession(TmuxName(id)) {
			st.Status = Done
		}
	}
	return st
}

func lookupPR(repo, branch string) *PR {
	out, err := exec.Command("gh", "pr", "list", "-R", repo, "--head", branch, "--state", "all",
		"--json", "number,url,state", "-L", "1").Output()
	if err != nil {
		return nil
	}
	var prs []PR
	if json.Unmarshal(out, &prs) != nil || len(prs) == 0 {
		return nil
	}
	return &prs[0]
}

// Peek is the last n lines of a session: the rendered stream, or the screen
// when someone has attached and a TUI owns it.
func Peek(id string, n int) ([]string, error) {
	st := liveState(id)
	if st.Status == Interactive {
		if pane := activePane(id); pane != "" {
			return tmuxctl.CapturePane(pane, n), nil
		}
	}
	data, err := os.ReadFile(filepath.Join(sessionDir(id), "log.jsonl"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	lines := Render(bytes.Split(data, []byte("\n")))
	if st.Status == Failed || st.Status == Died {
		if tail := tailFile(filepath.Join(sessionDir(id), "stderr.log"), 5); len(tail) > 0 {
			lines = append(lines, "stderr:")
			for _, l := range tail {
				lines = append(lines, "  "+l)
			}
		}
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

func activePane(id string) string {
	return tmuxctl.ActivePanes()[TmuxName(id)].ID
}

func tailFile(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// Send hands a session a follow-up. Into the TUI when someone has attached;
// otherwise into the inbox, where the runner takes it as the next turn,
// starting a runner when none is going.
func Send(id, message string) (string, error) {
	m, err := loadMeta(id)
	if err != nil {
		return "", err
	}
	st := liveState(id)
	if st.Status == Interactive {
		name := TmuxName(id)
		if err := exec.Command("tmux", "send-keys", "-t", "="+name+":", "-l", message).Run(); err != nil {
			return "", err
		}
		time.Sleep(200 * time.Millisecond)
		return "typed into the attached session", exec.Command("tmux", "send-keys", "-t", "="+name+":", "Enter").Run()
	}
	if err := enqueue(id, message); err != nil {
		return "", err
	}
	if runnerAlive(id) {
		return "queued; it is the next turn", nil
	}
	if st.Status == Killed {
		updateState(id, func(s *State) { s.Status = Done })
	}
	return "resumed", startRunner(m)
}

// Kill stops a session. With rm it also removes the worktree and the
// session's record; the branch and anything pushed stay.
func Kill(id string, rm bool) error {
	m, err := loadMeta(id)
	if err != nil {
		return err
	}
	updateState(id, func(s *State) { s.Status = Killed })
	tmuxctl.KillSession(TmuxName(id))
	stopRunners(id)
	for _, f := range inbox(id) {
		os.Remove(f)
	}
	if !rm {
		return nil
	}
	clone := filepath.Join(Home(), "repos", m.Repo)
	if out, err := git(clone, "worktree", "remove", "--force", m.Worktree); err != nil {
		return fmt.Errorf("worktree: %s", out)
	}
	return os.RemoveAll(sessionDir(id))
}

// Attach stops the runner and hands the session to the harness's own TUI in
// the same tmux session, resumed where the stream left off. It returns the
// tmux session for the caller to attach to. Detaching leaves the TUI running.
func Attach(id string) (string, error) {
	m, err := loadMeta(id)
	if err != nil {
		return "", err
	}
	name := TmuxName(id)
	st := liveState(id)
	if st.Status == Interactive {
		return name, nil
	}
	if st.HarnessSession == "" {
		return "", errors.New("the first turn has not started yet; try again in a moment")
	}
	stopRunners(id)
	updateState(id, func(s *State) { s.Status = Interactive })
	argv := interactiveCommand(m, st.HarnessSession)
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return name, newTmux(name, m, "exec "+strings.Join(quoted, " "))
}

func hostname() string {
	name, _ := os.Hostname()
	if i := strings.IndexByte(name, '.'); i >= 0 {
		name = name[:i]
	}
	return strings.ToLower(name)
}

var (
	loginOnce sync.Once
	login     string
)

func ghLogin() string {
	loginOnce.Do(func() {
		out, err := exec.Command("gh", "api", "user", "--jq", ".login").Output()
		if err == nil {
			login = strings.TrimSpace(string(out))
		}
	})
	return login
}
