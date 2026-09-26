// Command factory runs background coding agents on machines you own.
//
//	factory hosts                          each host: identity, headroom, live sessions
//	factory hosts add ALIAS | rm ALIAS     an ssh alias becomes a host, or stops being one
//	factory run [--on HOST] REPO TASK      a new session in its own worktree; prints its id
//	factory ls                             every session on every host
//	factory peek ID                        the recent transcript
//	factory send ID MESSAGE                a follow-up, taken as the session's next turn
//	factory wait ID...                     block until none of them is running
//	factory attach ID                      take over in the harness's own TUI, in tmux
//	factory kill ID [--rm]                 stop it; --rm also removes the worktree
//	factory find QUERY                     search every session's trace (hev kit)
//
// The laptop talks to its own sessions in-process and to every other host's
// by running `factory _host` there over ssh.
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

	"github.com/hev/factory/internal/fleet"
	"github.com/hev/factory/skills"
)

const usage = `factory: background coding agents on machines you own

  factory hosts                             each host: who it acts as, headroom, live sessions
  factory hosts add ALIAS                   make an ssh alias a host
  factory hosts rm ALIAS
  factory run [--on HOST] REPO TASK         a new session in its own worktree; prints its id
              [--harness claude|codex] [--model M] [--base BRANCH]
                                            REPO is OWNER/REPO, or a path whose origin is one;
                                            TASK "-" reads the task from stdin
  factory ls [--all] [--json] [--no-pr]     every session on every host
  factory peek ID [-n LINES]                the recent transcript
  factory send ID MESSAGE                   a follow-up, taken as the next turn ("-" reads stdin)
  factory wait ID... [--timeout 2h]         block until none of them is running
  factory attach ID                         take over in tmux; detach and it keeps running
  factory kill ID [--rm]                    stop it; --rm also removes its worktree
  factory find QUERY [--session ID] [...]   search every session's trace (hev query)
  factory skill install                     install the reception skill into ~/.claude/skills
  factory skill                             print it

A session acts as whoever its host is logged in as. Sessions run with every
approval off.
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
	switch cmd {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	case "version", "--version":
		fmt.Println(fleet.Version)
		return nil
	case "_host":
		return fleet.ServeStdin()
	case "_runner":
		if len(rest) != 1 {
			return errors.New("_runner ID")
		}
		return fleet.RunRunner(rest[0])
	case "hosts":
		return hosts(rest)
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
	case "find":
		return find(rest)
	case "skill":
		return skill(rest)
	case "loops", "loop":
		return errors.New("loops are not built yet; see the README")
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

func hosts(args []string) error {
	if len(args) >= 1 && (args[0] == "add" || args[0] == "rm") {
		if len(args) != 2 {
			return fmt.Errorf("hosts %s ALIAS", args[0])
		}
		if args[0] == "rm" {
			return fleet.RemoveHost(args[1])
		}
		info, err := fleet.AddHost(args[1])
		if err != nil {
			return err
		}
		fmt.Printf("%s added: acts as %s, %d cores, factory %s\n", args[1], orQ(info.Login), info.Cores, info.Version)
		return nil
	}
	opts, _, err := flags(args, nil, []string{"json"})
	if err != nil {
		return err
	}
	cands, err := survey()
	if err != nil {
		return err
	}
	if opts["json"] != "" {
		return printJSON(cands)
	}
	w := table()
	fmt.Fprintln(w, "HOST\tACTS AS\tLIVE\tLOAD\tMEM FREE\tCLAUDE WEEK\tCODEX WEEK\tFACTORY")
	for _, c := range cands {
		if c.Err != nil {
			fmt.Fprintf(w, "%s\t%s\n", c.Host.Name, "unreachable: "+c.Err.Error())
			continue
		}
		in := c.Info
		name := c.Host.Name
		if c.Host.Local() {
			name = "local (" + in.Name + ")"
		}
		mem := "?"
		if in.MemFreePct >= 0 {
			mem = fmt.Sprintf("%d%%", in.MemFreePct)
		}
		skew := ""
		if in.Version != fleet.Version {
			skew = " ≠ " + fleet.Version
		}
		fmt.Fprintf(w, "%s\t%s\t%d/%d\t%.1f/%d\t%s\t%s\t%s\t%s%s\n", name, orQ(in.Login),
			in.Live, fleet.Slots(c.Host, in), in.Load, in.Cores, mem,
			usageCell(in, "claude"), usageCell(in, "codex"), in.Version, skew)
	}
	return w.Flush()
}

func survey() ([]fleet.Candidate, error) {
	hs, err := fleet.Hosts()
	if err != nil {
		return nil, err
	}
	resps, errs := fleet.Each(hs, fleet.Request{Op: "info"})
	cands := make([]fleet.Candidate, len(hs))
	for i, h := range hs {
		cands[i] = fleet.Candidate{Host: h, Info: resps[i].Info, Err: errs[i]}
	}
	return cands, nil
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
	opts, rest, err := flags(args, []string{"on", "harness", "model", "base"}, nil)
	if err != nil {
		return err
	}
	if len(rest) < 2 {
		return errors.New(`run [--on HOST] REPO "TASK"`)
	}
	repo, err := repoName(rest[0])
	if err != nil {
		return err
	}
	task, err := textArg(strings.Join(rest[1:], " "))
	if err != nil {
		return err
	}
	harness := opts["harness"]
	if harness == "" {
		harness = "claude"
	}
	if harness != "claude" && harness != "codex" {
		return fmt.Errorf("harness is claude or codex, not %q", harness)
	}
	var host fleet.Host
	if on := opts["on"]; on != "" {
		hs, err := fleet.Hosts()
		if err != nil {
			return err
		}
		found := false
		for _, h := range hs {
			if h.Name == on || (h.Local() && on == localName()) {
				host, found = h, true
			}
		}
		if !found {
			return fmt.Errorf("no host %q; `factory hosts` lists them", on)
		}
	} else {
		cands, err := survey()
		if err != nil {
			return err
		}
		if host, err = fleet.Place(cands, harness); err != nil {
			return err
		}
	}
	resp, err := host.Call(fleet.Request{Op: "start", Start: &fleet.StartRequest{
		Repo: repo, Task: task, Harness: harness, Model: opts["model"], Base: opts["base"],
	}})
	if err != nil {
		return err
	}
	m := resp.Meta
	fmt.Println(m.ID)
	fmt.Fprintf(os.Stderr, "on %s as %s, %s in %s, branch %s from %s\n", host.Name, orQ(m.Login), m.Harness, m.Repo, m.Branch, m.Base)
	return nil
}

func localName() string {
	name, _ := os.Hostname()
	name, _, _ = strings.Cut(name, ".")
	return strings.ToLower(name)
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

type row struct {
	fleet.Session
	HostName string `json:"host_name"`
}

func ls(args []string) error {
	opts, _, err := flags(args, nil, []string{"all", "json", "no-pr"})
	if err != nil {
		return err
	}
	hs, err := fleet.Hosts()
	if err != nil {
		return err
	}
	resps, errs := fleet.Each(hs, fleet.Request{Op: "list", PRs: opts["no-pr"] == ""})
	var rows []row
	for i, h := range hs {
		if errs[i] != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", h.Name, errs[i])
			continue
		}
		for _, s := range resps[i].Sessions {
			live := s.Status == fleet.Running || s.Status == fleet.Interactive
			if opts["all"] == "" && !live && time.Since(s.UpdatedAt) > 72*time.Hour {
				continue
			}
			rows = append(rows, row{Session: s, HostName: h.Name})
		}
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
	fmt.Fprintln(w, "ID\tHOST\tSTATUS\tAGE\tREPO\tPR\tTASK")
	for _, r := range rows {
		status := r.Status
		if r.Queued > 0 {
			status += fmt.Sprintf(" +%d", r.Queued)
		}
		pr := "-"
		if r.PR != nil {
			pr = fmt.Sprintf("#%d %s", r.PR.Number, strings.ToLower(r.PR.State))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.HostName, status, age(r.CreatedAt),
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

// onSession finds the session's host and runs one request there.
func onSession(id string, req fleet.Request) (fleet.Host, fleet.Response, error) {
	host, err := fleet.Find(id)
	if err != nil {
		return host, fleet.Response{}, err
	}
	req.ID = id
	resp, err := host.Call(req)
	return host, resp, err
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
	host, resp, err := onSession(rest[0], fleet.Request{Op: "peek", Lines: n})
	if err != nil {
		return err
	}
	if m := resp.Meta; m != nil {
		fmt.Printf("%s on %s · %s · %s · %s\n\n", m.ID, host.Name, m.Repo, m.Branch, m.Harness)
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
	_, resp, err := onSession(args[0], fleet.Request{Op: "send", Message: msg})
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
	hs, err := fleet.Hosts()
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for {
		resps, errs := fleet.Each(hs, fleet.Request{Op: "list"})
		ended := map[string]row{}
		pending := 0
		for _, id := range ids {
			found := false
			for i, h := range hs {
				if errs[i] != nil {
					continue
				}
				for _, s := range resps[i].Sessions {
					if strings.HasPrefix(s.ID, id) {
						found = true
						if s.Status == fleet.Running {
							pending++
						} else {
							ended[id] = row{Session: s, HostName: h.Name}
						}
					}
				}
			}
			if !found {
				unreachable := false
				for _, e := range errs {
					unreachable = unreachable || e != nil
				}
				if !unreachable {
					return fmt.Errorf("no session %q", id)
				}
				pending++ // its host may be back in a moment
			}
		}
		if pending == 0 {
			for _, id := range ids {
				r := ended[id]
				fmt.Printf("%s\t%s\t%s\t%s\n", r.ID, r.HostName, r.Status, oneLine(r.Result, 200))
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
	host, resp, err := onSession(args[0], fleet.Request{Op: "attach"})
	if err != nil {
		return err
	}
	return fleet.AttachTmux(host, resp.Tmux)
}

func kill(args []string) error {
	opts, rest, err := flags(args, nil, []string{"rm"})
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("kill ID [--rm]")
	}
	_, _, err = onSession(rest[0], fleet.Request{Op: "kill", Rm: opts["rm"] != ""})
	return err
}

// find is hev kit's search. Every host captures into one namespace, so the
// laptop's `hev query` already spans them; --session narrows it to one
// session's worktree.
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
			_, resp, err := onSession(id, fleet.Request{Op: "peek", Lines: 1})
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

func table() *tabwriter.Writer { return tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0) }

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
