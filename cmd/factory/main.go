// Command factory runs background coding agents on a machine you own.
//
//	factory host                           this machine: identity, headroom, live sessions
//	factory run REPO TASK                  a new session in its own worktree; prints its id
//	factory ls                             every session
//	factory peek ID                        the recent transcript
//	factory send ID MESSAGE                a follow-up, taken as the session's next turn
//	factory wait ID...                     block until none of them is running
//	factory attach ID                      take over in the harness's own TUI, in tmux
//	factory kill ID [--rm]                 stop it; --rm also removes the worktree
//	factory find QUERY                     search every session's trace (hev kit)
//
// It works on the machine it runs on. With an executable named
// factory-remote on PATH, it is a client instead: every command but a few
// local ones is handed to factory-remote, which runs it on the server.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/hev/factory/internal/fleet"
	"github.com/hev/factory/internal/privacy"
	"github.com/hev/factory/skills"
)

const usage = `factory: background coding agents on a machine you own

  factory host                              this machine: who it acts as, headroom, live sessions
  factory run REPO TASK                     a new session in its own worktree; prints its id
              [--harness claude|codex] [--model M] [--base BRANCH]
                                            REPO is OWNER/REPO, or a path whose origin is one;
                                            TASK "-" reads the task from stdin
  factory ls [--all] [--json] [--no-pr]     every session
  factory peek ID [-n LINES]                the recent transcript
  factory send ID MESSAGE                   a follow-up, taken as the next turn ("-" reads stdin)
  factory wait ID... [--timeout 2h]         block until none of them is running
  factory attach ID                         take over in tmux; detach and it keeps running
  factory kill ID [--rm]                    stop it; --rm also removes its worktree
  factory privacy plan|apply OFFLINE_ROOT INPUT_JSON OUTPUT_JSON
                                            bounded offline artifact remediation; see docs/privacy.md
  factory find QUERY [--session ID] [...]   search every session's trace (hev query)
  factory job add [--line L] [--done-when CMD] [--spec FILE] ASK
                                            file a job ("-" reads the ask from stdin)
  factory job show ID                       its spec, state and latest log
  factory job say ID TEXT                   tell the job's gaffer something; it hears it on the next tick
  factory job ceiling ID [--wakes N] [--days N]   raise its ceiling (reopens a job the ceiling stopped)
  factory job done|stop|open ID [NOTE]      settle it by hand
  factory job part-add ID NAME REPO TASK [--after a,b]   add a part (the gaffer's split)
  factory job wait ID TEXT                  mark it waiting on the operator (the gaffer, usually)
  factory job progress ID TEXT              post a milestone to the job's Slack thread (the gaffer)
  factory run --job ID --part NAME          start a part; refused until its after list has merged
  factory jobs [--all] [--json]             every job
  factory foreman start --model M           start the persistent floor supervisor
  factory foreman status|peek|say MESSAGE    inspect or direct the foreman
  factory foreman stop|watch                 stop, or run the host supervisor
  factory tick                              the clock: notice deaths, read new events (run every minute; no model)
  factory skill install                     install the reception skill into ~/.claude/skills
  factory skill                             print it

A session acts as whoever this machine is logged in as. Sessions run with
every approval off.

With factory-remote on PATH this is a client: every command but help,
version and skill runs on the server through it. --local, first, runs one
here instead.
`

func main() {
	fleet.Version = version()
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "factory: "+err.Error())
		os.Exit(1)
	}
}

func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if rev == "" {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
		return "dev"
	}
	if dirty {
		rev += "+"
	}
	return "dev-" + rev
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	cmd, rest := args[0], args[1:]
	if cmd == "--local" {
		if len(rest) == 0 {
			return errors.New("--local VERB ...")
		}
		cmd, rest = rest[0], rest[1:]
	} else if remote, err := exec.LookPath("factory-remote"); err == nil && owned[cmd] {
		// A client: the work lives on the server, so the command runs there.
		return syscall.Exec(remote, append([]string{remote}, args...), os.Environ())
	}
	switch cmd {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	case "version", "--version":
		fmt.Println(fleet.Version)
		return nil
	case "_runner":
		if len(rest) != 1 {
			return errors.New("_runner ID")
		}
		return fleet.RunRunner(rest[0])
	case "host", "hosts":
		return host(rest)
	case "run":
		return runSession(rest)
	case "ls":
		return ls(rest)
	case "peek":
		return peek(rest)
	case "send":
		return send(rest)
	case "wait":
		return wait(rest)
	case "attach":
		return attach(rest)
	case "kill":
		return kill(rest)
	case "privacy":
		if len(rest) != 4 {
			return errors.New("privacy plan|apply OFFLINE_ROOT INPUT_JSON OUTPUT_JSON")
		}
		p, e := privacy.Execute(rest[0], rest[1], rest[2], rest[3])
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(p)
	case "find":
		return find(rest)
	case "skill":
		return skill(rest)
	case "foreman":
		return fleet.Foreman(rest)
	case "tick":
		return tick(rest)
	case "job":
		return job(rest)
	case "jobs":
		return jobs(rest)
	case "loops", "loop":
		return errors.New("loops run on hev loop, not in factory; see the README")
	}
	// The git shape: a verb this binary does not own is an executable named
	// factory-<verb> on PATH. It is how another build adds a verb without a
	// fork, and this file never learns what the verb does.
	if !strings.HasPrefix(cmd, "-") {
		if path, err := exec.LookPath("factory-" + cmd); err == nil {
			return syscall.Exec(path, append([]string{path}, rest...), os.Environ())
		}
	}
	return fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
}

// owned is every verb a client hands to factory-remote: the ones this binary
// owns that act on sessions and jobs. help, version and skill are about this
// machine's copy, and a factory-<verb> from PATH decides for itself.
var owned = map[string]bool{
	"privacy": true, "host": true, "hosts": true, "run": true, "ls": true, "peek": true, "send": true,
	"wait": true, "attach": true, "kill": true, "find": true, "tick": true, "job": true, "jobs": true, "foreman": true,
}

// flags pulls --name value / --name=value / --switch out of args wherever
// they are, and returns the rest in order. Anything after "--" is left alone.
func flags(args []string, valued, switches []string) (map[string]string, []string, error) {
	got := map[string]string{}
	var rest []string
	isIn := func(list []string, s string) bool {
		for _, v := range list {
			if v == s {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			rest = append(rest, a)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		switch {
		case isIn(switches, name):
			got[name] = "true"
		case isIn(valued, name):
			if !hasValue {
				if i+1 >= len(args) {
					return nil, nil, fmt.Errorf("%s needs a value", a)
				}
				i++
				value = args[i]
			}
			got[name] = value
		default:
			return nil, nil, fmt.Errorf("unknown flag %s", a)
		}
	}
	return got, rest, nil
}

func host(args []string) error {
	opts, _, err := flags(args, nil, []string{"json"})
	if err != nil {
		return err
	}
	resp, err := fleet.Call(fleet.Request{Op: "info"})
	if err != nil {
		return err
	}
	in := resp.Info
	if opts["json"] != "" {
		return printJSON(in)
	}
	mem := "?"
	if in.MemFreePct >= 0 {
		mem = fmt.Sprintf("%d%%", in.MemFreePct)
	}
	w := table()
	fmt.Fprintln(w, "HOST\tACTS AS\tLIVE\tLOAD\tMEM FREE\tCLAUDE WEEK\tCODEX WEEK\tFACTORY")
	fmt.Fprintf(w, "%s\t%s\t%d/%d\t%.1f/%d\t%s\t%s\t%s\t%s\n", in.Name, orQ(in.Login),
		in.Live, fleet.Slots(*in), in.Load, in.Cores, mem,
		usageCell(in, "claude"), usageCell(in, "codex"), in.Version)
	return w.Flush()
}

func usageCell(in *fleet.Info, harness string) string {
	has := false
	for _, h := range in.Harnesses {
		has = has || h == harness
	}
	if !has {
		return "-"
	}
	u := in.Usage[harness]
	if u == nil {
		return "?"
	}
	return fmt.Sprintf("%.0f%% (resets %s)", u.UsedPct, u.ResetsAt.Local().Format("Mon 15:04"))
}

func orQ(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

func runSession(args []string) error {
	opts, rest, err := flags(args, []string{"harness", "model", "base", "job", "part"}, nil)
	if err != nil {
		return err
	}
	if opts["job"] != "" || opts["part"] != "" {
		return runPart(opts, rest)
	}
	if len(rest) < 2 {
		return errors.New(`run REPO "TASK"`)
	}
	repo, err := repoName(rest[0])
	if err != nil {
		return err
	}
	task, err := textArg(strings.Join(rest[1:], " "))
	if err != nil {
		return err
	}
	harness, _ := fleet.WorkerDefaults(opts["harness"], opts["model"])
	if harness != "claude" && harness != "codex" {
		return fmt.Errorf("harness is claude or codex, not %q", harness)
	}
	info, err := fleet.Call(fleet.Request{Op: "info"})
	if err != nil {
		return err
	}
	if err := fleet.Room(*info.Info, harness); err != nil {
		return err
	}
	resp, err := fleet.Call(fleet.Request{Op: "start", Start: &fleet.StartRequest{
		Repo: repo, Task: task, Harness: opts["harness"], Model: opts["model"], Base: opts["base"],
	}})
	if err != nil {
		return err
	}
	m := resp.Meta
	fmt.Println(m.ID)
	fmt.Fprintf(os.Stderr, "on %s as %s, %s in %s, branch %s from %s\n", m.Host, orQ(m.Login), m.Harness, m.Repo, m.Branch, m.Base)
	return nil
}

// runPart starts a job's part. The part's own
// repo and task apply unless REPO and TASK are given.
func runPart(opts map[string]string, rest []string) error {
	if opts["job"] == "" || opts["part"] == "" {
		return errors.New("run --job ID --part NAME [REPO TASK]")
	}
	req := fleet.StartRequest{Job: opts["job"], Part: opts["part"], Harness: opts["harness"], Model: opts["model"], Base: opts["base"]}
	if len(rest) > 0 {
		repo, err := repoName(rest[0])
		if err != nil {
			return err
		}
		req.Repo = repo
		if len(rest) > 1 {
			if req.Task, err = textArg(strings.Join(rest[1:], " ")); err != nil {
				return err
			}
		}
	}
	resp, err := fleet.Call(fleet.Request{Op: "start", Start: &req})
	if err != nil {
		return err
	}
	m := resp.Meta
	fmt.Println(m.ID)
	fmt.Fprintf(os.Stderr, "part %s of job %s on %s as %s, %s in %s, branch %s\n", m.Part, m.Job, m.Host, orQ(m.Login), m.Harness, m.Repo, m.Branch)
	return nil
}

var githubRemote = regexp.MustCompile(`github\.com[:/]([^/]+/[^/]+?)(\.git)?/?$`)

// repoName takes OWNER/REPO as given, or reads it from a checkout's origin.
func repoName(arg string) (string, error) {
	if st, err := os.Stat(arg); err == nil && st.IsDir() {
		out, err := exec.Command("git", "-C", arg, "remote", "get-url", "origin").Output()
		if err != nil {
			return "", fmt.Errorf("%s has no origin remote", arg)
		}
		m := githubRemote.FindStringSubmatch(strings.TrimSpace(string(out)))
		if m == nil {
			return "", fmt.Errorf("%s's origin is not a GitHub repo", arg)
		}
		return m[1], nil
	}
	if strings.Count(arg, "/") == 1 {
		return arg, nil
	}
	return "", fmt.Errorf("repo is OWNER/REPO or a checkout, not %q", arg)
}

func textArg(s string) (string, error) {
	if s != "-" {
		return s, nil
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", errors.New("nothing on stdin")
	}
	return string(data), nil
}

func ls(args []string) error {
	opts, _, err := flags(args, nil, []string{"all", "json", "no-pr"})
	if err != nil {
		return err
	}
	resp, err := fleet.Call(fleet.Request{Op: "list", PRs: opts["no-pr"] == ""})
	if err != nil {
		return err
	}
	var rows []fleet.Session
	for _, s := range resp.Sessions {
		live := s.Status == fleet.Running || s.Status == fleet.Interactive
		if opts["all"] == "" && !live && time.Since(s.UpdatedAt) > 72*time.Hour {
			continue
		}
		rows = append(rows, s)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.After(rows[j].CreatedAt) })
	if opts["json"] != "" {
		return printJSON(rows)
	}
	if len(rows) == 0 {
		fmt.Println("no sessions")
		return nil
	}
	w := table()
	fmt.Fprintln(w, "ID\tSTATUS\tAGE\tREPO\tPR\tTASK")
	for _, r := range rows {
		status := r.Status
		if r.Queued > 0 {
			status += fmt.Sprintf(" +%d", r.Queued)
		}
		pr := "-"
		if r.PR != nil {
			pr = fmt.Sprintf("#%d %s", r.PR.Number, strings.ToLower(r.PR.State))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, status, age(r.CreatedAt),
			r.Repo, pr, oneLine(r.Task, 60))
	}
	return w.Flush()
}

func age(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// onSession runs one request on a session, by id or unique id prefix.
func onSession(id string, req fleet.Request) (fleet.Response, error) {
	req.ID = id
	return fleet.Call(req)
}

func peek(args []string) error {
	opts, rest, err := flags(args, []string{"n"}, nil)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("peek ID [-n LINES]")
	}
	n := 40
	if v := opts["n"]; v != "" {
		if n, err = strconv.Atoi(v); err != nil {
			return fmt.Errorf("-n %q", v)
		}
	}
	resp, err := onSession(rest[0], fleet.Request{Op: "peek", Lines: n})
	if err != nil {
		return err
	}
	if m := resp.Meta; m != nil {
		fmt.Printf("%s on %s · %s · %s · %s\n\n", m.ID, m.Host, m.Repo, m.Branch, m.Harness)
	}
	for _, l := range resp.Lines {
		fmt.Println(l)
	}
	return nil
}

func send(args []string) error {
	if len(args) < 2 {
		return errors.New("send ID MESSAGE")
	}
	msg, err := textArg(strings.Join(args[1:], " "))
	if err != nil {
		return err
	}
	resp, err := onSession(args[0], fleet.Request{Op: "send", Message: msg})
	if err != nil {
		return err
	}
	fmt.Println(resp.Note)
	return nil
}

// wait blocks until every named session has stopped running, then prints
// where each ended. It is what a coordinator runs in the background instead
// of polling ls itself.
func wait(args []string) error {
	opts, ids, err := flags(args, []string{"timeout", "every"}, nil)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return errors.New("wait ID... [--timeout 2h]")
	}
	timeout, every := 2*time.Hour, 30*time.Second
	if v := opts["timeout"]; v != "" {
		if timeout, err = time.ParseDuration(v); err != nil {
			return err
		}
	}
	if v := opts["every"]; v != "" {
		if every, err = time.ParseDuration(v); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(timeout)
	for {
		resp, err := fleet.Call(fleet.Request{Op: "list"})
		if err != nil {
			return err
		}
		ended := map[string]fleet.Session{}
		pending := 0
		for _, id := range ids {
			found := false
			for _, s := range resp.Sessions {
				if strings.HasPrefix(s.ID, id) {
					found = true
					if s.Status == fleet.Running {
						pending++
					} else {
						ended[id] = s
					}
				}
			}
			if !found {
				return fmt.Errorf("no session %q", id)
			}
		}
		if pending == 0 {
			for _, id := range ids {
				s := ended[id]
				fmt.Printf("%s\t%s\t%s\n", s.ID, s.Status, oneLine(s.Result, 200))
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%d of %d still running after %s", pending, len(ids), timeout)
		}
		time.Sleep(every)
	}
}

func attach(args []string) error {
	if len(args) != 1 {
		return errors.New("attach ID")
	}
	resp, err := onSession(args[0], fleet.Request{Op: "attach"})
	if err != nil {
		return err
	}
	return fleet.AttachTmux(resp.Tmux)
}

func kill(args []string) error {
	opts, rest, err := flags(args, nil, []string{"rm"})
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("kill ID [--rm]")
	}
	_, err = onSession(rest[0], fleet.Request{Op: "kill", Rm: opts["rm"] != ""})
	return err
}

// find is hev kit's search. Every machine captures into one namespace, so
// `hev query` spans them all; --session narrows it to one session's worktree.
func find(args []string) error {
	var pass []string
	for i := 0; i < len(args); i++ {
		if a := args[i]; a == "--session" || strings.HasPrefix(a, "--session=") {
			id, ok := strings.CutPrefix(a, "--session=")
			if !ok {
				if i+1 >= len(args) {
					return errors.New("--session needs an id")
				}
				i++
				id = args[i]
			}
			resp, err := onSession(id, fleet.Request{Op: "peek", Lines: 1})
			if err != nil {
				return err
			}
			pass = append(pass, "--workdir", resp.Meta.Worktree)
			continue
		}
		pass = append(pass, args[i])
	}
	if len(pass) == 0 {
		return errors.New("find QUERY")
	}
	path, err := exec.LookPath("hev")
	if err != nil {
		return errors.New("find needs hev kit (`hev`) on this machine")
	}
	return syscall.Exec(path, append([]string{"hev", "query"}, pass...), os.Environ())
}

// skill installs the reception skill this build carries, so the model on the
// laptop is always taught the verbs this binary actually has.
func skill(args []string) error {
	body, err := skills.FS.ReadFile("reception/SKILL.md")
	if err != nil {
		return err
	}
	if len(args) == 0 {
		_, err := os.Stdout.Write(body)
		return err
	}
	if args[0] != "install" || len(args) > 1 {
		return errors.New("skill [install]")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".claude", "skills", "reception")
	path := filepath.Join(dir, "SKILL.md")
	if old, err := os.ReadFile(path); err == nil {
		if string(old) == string(body) {
			fmt.Println(path + " is current")
			return nil
		}
		if !strings.Contains(string(old), "factory run") {
			// The front-desk skill this replaces. Kept beside it, not lost.
			if err := os.WriteFile(path+".front-desk", old, 0o644); err != nil {
				return err
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return err
	}
	fmt.Println("installed " + path)
	return nil
}

// job files and reads jobs on this machine, the one that owns them. From a
// client, the whole command has already gone to the server.
func job(args []string) error {
	if len(args) == 0 {
		return errors.New("job add|show")
	}
	switch args[0] {
	case "add":
		opts, rest, err := flags(args[1:], []string{"line", "done-when", "spec", "wakes", "days", "source"}, []string{"json"})
		if err != nil {
			return err
		}
		var spec fleet.JobSpec
		if f := opts["spec"]; f != "" {
			if spec, err = readSpec(f); err != nil {
				return err
			}
		}
		if len(rest) > 0 {
			if spec.Ask, err = textArg(strings.Join(rest, " ")); err != nil {
				return err
			}
		}
		for flag, dst := range map[string]*string{"line": &spec.Line, "done-when": &spec.DoneWhen, "source": &spec.Source} {
			if v := opts[flag]; v != "" {
				*dst = v
			}
		}
		for flag, dst := range map[string]*int{"wakes": &spec.Ceiling.Wakes, "days": &spec.Ceiling.Days} {
			if v := opts[flag]; v != "" {
				if *dst, err = strconv.Atoi(v); err != nil {
					return fmt.Errorf("--%s %q", flag, v)
				}
			}
		}
		resp, err := fleet.Call(fleet.Request{Op: "job_add", Job: &spec})
		if err != nil {
			return err
		}
		if opts["json"] != "" {
			return printJSON(resp.Job)
		}
		j := resp.Job
		fmt.Println(j.ID)
		fmt.Fprintf(os.Stderr, "filed: %d parts, ceiling %d wakes / %d days\n", len(j.Parts), j.Ceiling.Wakes, j.Ceiling.Days)
		return nil
	case "show":
		opts, rest, err := flags(args[1:], []string{"n"}, []string{"json"})
		if err != nil {
			return err
		}
		if len(rest) != 1 {
			return errors.New("job show ID")
		}
		n := 10
		if v := opts["n"]; v != "" {
			if n, err = strconv.Atoi(v); err != nil {
				return fmt.Errorf("-n %q", v)
			}
		}
		resp, err := fleet.Call(fleet.Request{Op: "job_show", ID: rest[0], Lines: n})
		if err != nil {
			return err
		}
		if opts["json"] != "" {
			return printJSON(map[string]any{"job": resp.Job, "log": resp.Log})
		}
		printJob(resp.Job, resp.Log)
		return nil
	case "part-add":
		opts, rest, err := flags(args[1:], []string{"after", "line"}, nil)
		if err != nil {
			return err
		}
		if len(rest) < 4 {
			return errors.New(`job part-add ID NAME OWNER/REPO "TASK" [--after a,b]`)
		}
		task, err := textArg(strings.Join(rest[3:], " "))
		if err != nil {
			return err
		}
		p := fleet.Part{Name: rest[1], Repo: rest[2], Task: task, Line: opts["line"]}
		if a := opts["after"]; a != "" {
			p.After = strings.Split(a, ",")
		}
		return jobChange(fleet.Request{Op: "job_part_add", ID: rest[0], Part: &p})
	case "wait", "say", "progress", "log":
		if len(args) < 3 {
			return fmt.Errorf("job %s ID TEXT", args[0])
		}
		text, err := textArg(strings.Join(args[2:], " "))
		if err != nil {
			return err
		}
		op := map[string]string{"wait": "job_status", "say": "job_say", "progress": "job_progress", "log": "job_log"}[args[0]]
		req := fleet.Request{Op: op, ID: args[1], Message: text}
		if args[0] == "wait" {
			req.Status = fleet.JobWaiting
		}
		return jobChange(req)
	case "open", "done", "stop":
		if len(args) < 2 {
			return fmt.Errorf("job %s ID [NOTE]", args[0])
		}
		status := map[string]string{"open": fleet.JobOpen, "done": fleet.JobDone, "stop": fleet.JobStopped}[args[0]]
		return jobChange(fleet.Request{Op: "job_status", ID: args[1], Status: status, Message: strings.Join(args[2:], " ")})
	case "ceiling":
		opts, rest, err := flags(args[1:], []string{"wakes", "days"}, nil)
		if err != nil {
			return err
		}
		if len(rest) != 1 || (opts["wakes"] == "" && opts["days"] == "") {
			return errors.New("job ceiling ID [--wakes N] [--days N]")
		}
		req := fleet.Request{Op: "job_ceiling", ID: rest[0]}
		if v := opts["wakes"]; v != "" {
			if req.Wakes, err = strconv.Atoi(v); err != nil {
				return fmt.Errorf("--wakes %q", v)
			}
		}
		if v := opts["days"]; v != "" {
			if req.Days, err = strconv.Atoi(v); err != nil {
				return fmt.Errorf("--days %q", v)
			}
		}
		return jobChange(req)
	}
	return fmt.Errorf("job %q: add, show, part-add, say, progress, wait, open, done, stop, ceiling or log", args[0])
}

// jobChange makes one write to a job and prints the job's status.
func jobChange(req fleet.Request) error {
	req.Who = fleet.WhoAmI()
	resp, err := fleet.Call(req)
	if err != nil {
		return err
	}
	j := resp.Job
	fmt.Printf("job %s: %s", j.ID, j.State.Status)
	if j.State.WaitingOnYou != "" {
		fmt.Printf(" (waiting on you: %s)", j.State.WaitingOnYou)
	}
	fmt.Printf(", %d parts, %d/%d wakes\n", len(j.Parts), j.State.Wakes, j.Ceiling.Wakes)
	return nil
}

// readSpec reads a job spec from a file (or stdin for "-"): TOML with the
// same fields as job.md's front matter, plus ask.
func readSpec(path string) (fleet.JobSpec, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return fleet.JobSpec{}, err
	}
	var spec struct {
		fleet.JobSpec
		Ask string `toml:"ask"`
	}
	if _, err := toml.Decode(string(data), &spec); err != nil {
		return fleet.JobSpec{}, fmt.Errorf("%s: %w", path, err)
	}
	spec.JobSpec.Ask = spec.Ask
	return spec.JobSpec, nil
}

func printJob(j *fleet.Job, log string) {
	st := j.State
	fmt.Printf("job %s · %s · created %s", j.ID, st.Status, j.Created.Local().Format("Jan 2 15:04"))
	if j.Line != "" {
		fmt.Printf(" · line %s", j.Line)
	}
	fmt.Printf(" · %d/%d wakes\n\n%s\n", st.Wakes, j.Ceiling.Wakes, j.Ask)
	if st.WaitingOnYou != "" {
		fmt.Printf("\nWaiting on you: %s\n", st.WaitingOnYou)
	}
	if st.BlockedOn != "" {
		fmt.Printf("\nBlocked on: %s\n", st.BlockedOn)
	}
	if j.DoneWhen != "" {
		fmt.Printf("\nDone when: %s\n", j.DoneWhen)
	}
	if len(j.Parts) > 0 {
		fmt.Println()
		w := table()
		fmt.Fprintln(w, "PART\tREPO\tAFTER\tSTATUS\tSESSION\tPR")
		for _, p := range j.Parts {
			ps := st.Parts[p.Name]
			pr := "-"
			if ps.PR != nil {
				pr = fmt.Sprintf("#%d %s", ps.PR.Number, strings.ToLower(ps.PR.State))
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", p.Name, p.Repo, orDash(strings.Join(p.After, ",")),
				orDash(ps.Status), orDash(ps.Session), pr)
		}
		w.Flush()
	}
	if log != "" {
		fmt.Printf("\n%s\n", log)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func jobs(args []string) error {
	opts, _, err := flags(args, nil, []string{"all", "json"})
	if err != nil {
		return err
	}
	resp, err := fleet.Call(fleet.Request{Op: "jobs"})
	if err != nil {
		return err
	}
	var rows []fleet.Job
	for _, j := range resp.Jobs {
		closed := j.State.Status == fleet.JobDone || j.State.Status == fleet.JobStopped
		if opts["all"] == "" && closed && time.Since(j.State.UpdatedAt) > 72*time.Hour {
			continue
		}
		rows = append(rows, j)
	}
	if opts["json"] != "" {
		return printJSON(rows)
	}
	if len(rows) == 0 {
		fmt.Println("no jobs")
		return nil
	}
	w := table()
	fmt.Fprintln(w, "ID\tSTATUS\tLINE\tPARTS\tWAKES\tAGE\tASK")
	for _, j := range rows {
		merged := 0
		for _, ps := range j.State.Parts {
			if ps.Status == "merged" {
				merged++
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d/%d merged\t%d/%d\t%s\t%s\n", j.ID, j.State.Status, orDash(j.Line),
			merged, len(j.Parts), j.State.Wakes, j.Ceiling.Wakes, age(j.Created), oneLine(j.Ask, 60))
	}
	return w.Flush()
}

// tick runs one tick on this machine. It is meant for launchd every minute,
// on the machine that owns the jobs, and prints what it saw.
func tick(args []string) error {
	opts, _, err := flags(args, nil, []string{"quiet"})
	if err != nil {
		return err
	}
	rep, err := fleet.Tick()
	if errors.Is(err, fleet.ErrTickBusy) {
		return nil // the previous minute's tick is still going; this one is redundant
	}
	if err != nil {
		return err
	}
	if opts["quiet"] == "" {
		for _, e := range rep.Events {
			fmt.Println(fleet.EventLine(e))
		}
		for _, id := range rep.Filed {
			fmt.Println(fleet.FiledLine(id))
		}
		for _, w := range rep.Wakes {
			fmt.Println(fleet.WakeLine(w))
		}
	}
	return nil
}

func table() *tabwriter.Writer { return tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0) }

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
