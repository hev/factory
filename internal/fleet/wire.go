package fleet

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// Request is one operation on one host. The laptop sends it to its own
// Handle directly and to every other host's over ssh, so there is one code
// path and a remote host is only a slower local one.
type Request struct {
	Op      string        `json:"op"` // info, start, list, peek, send, kill, attach
	ID      string        `json:"id,omitempty"`
	Start   *StartRequest `json:"start,omitempty"`
	Lines   int           `json:"lines,omitempty"`
	Message string        `json:"message,omitempty"`
	Rm      bool          `json:"rm,omitempty"`
	PRs     bool          `json:"prs,omitempty"`
}

// Response carries whichever field the op fills.
type Response struct {
	Error    string    `json:"error,omitempty"`
	NotFound bool      `json:"not_found,omitempty"`
	Info     *Info     `json:"info,omitempty"`
	Meta     *Meta     `json:"meta,omitempty"`
	Sessions []Session `json:"sessions,omitempty"`
	Lines    []string  `json:"lines,omitempty"`
	Note     string    `json:"note,omitempty"`
	Tmux     string    `json:"tmux,omitempty"`
}

// Handle answers a request on this host.
func Handle(req Request) Response {
	var resp Response
	fail := func(err error) Response {
		return Response{Error: err.Error(), NotFound: errors.Is(err, ErrNoSession)}
	}
	var id string
	if req.ID != "" {
		var err error
		if id, err = Resolve(req.ID); err != nil {
			return fail(err)
		}
	}
	switch req.Op {
	case "info":
		info := HostInfo()
		resp.Info = &info
	case "start":
		if req.Start == nil {
			return fail(errors.New("start: no request"))
		}
		m, err := Start(*req.Start)
		if err != nil {
			return fail(err)
		}
		resp.Meta = &m
	case "list":
		sessions, err := List(req.PRs)
		if err != nil {
			return fail(err)
		}
		resp.Sessions = sessions
	case "peek":
		lines, err := Peek(id, req.Lines)
		if err != nil {
			return fail(err)
		}
		resp.Lines = lines
	case "send":
		note, err := Send(id, req.Message)
		if err != nil {
			return fail(err)
		}
		resp.Note = note
	case "kill":
		if err := Kill(id, req.Rm); err != nil {
			return fail(err)
		}
	case "attach":
		name, err := Attach(id)
		if err != nil {
			return fail(err)
		}
		resp.Tmux = name
	default:
		return fail(fmt.Errorf("unknown op %q", req.Op))
	}
	if req.ID != "" && resp.Meta == nil {
		if m, err := loadMeta(id); err == nil {
			resp.Meta = &m
		}
	}
	return resp
}

// Host is a machine sessions can run on. SSH is its ssh alias; empty means
// this machine.
type Host struct {
	Name string
	SSH  string
	Max  int // live sessions it takes; 0 means half its cores
}

func (h Host) Local() bool { return h.SSH == "" }

// Hosts is this machine followed by every host in ~/.factory/hosts, one ssh
// alias per line, optionally with max=N.
func Hosts() ([]Host, error) {
	hosts := []Host{{Name: "local"}}
	data, err := os.ReadFile(hostsFile())
	if errors.Is(err, os.ErrNotExist) {
		return hosts, nil
	}
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		h := Host{Name: f[0], SSH: f[0]}
		for _, opt := range f[1:] {
			if v, ok := strings.CutPrefix(opt, "max="); ok {
				fmt.Sscan(v, &h.Max)
			}
		}
		hosts = append(hosts, h)
	}
	return hosts, sc.Err()
}

func hostsFile() string { return filepath.Join(Home(), "hosts") }

// AddHost records an ssh alias as a host, after checking it answers.
func AddHost(alias string) (Info, error) {
	h := Host{Name: alias, SSH: alias}
	resp, err := h.Call(Request{Op: "info"})
	if err != nil {
		return Info{}, err
	}
	hosts, _ := Hosts()
	for _, existing := range hosts {
		if existing.SSH == alias {
			return *resp.Info, nil
		}
	}
	if err := os.MkdirAll(Home(), 0o755); err != nil {
		return Info{}, err
	}
	f, err := os.OpenFile(hostsFile(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, alias)
	return *resp.Info, err
}

// RemoveHost forgets an ssh alias. Its sessions keep running; this machine
// just stops asking about them.
func RemoveHost(alias string) error {
	data, err := os.ReadFile(hostsFile())
	if err != nil {
		return err
	}
	var keep []string
	found := false
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if f := strings.Fields(line); len(f) > 0 && f[0] == alias {
			found = true
			continue
		}
		keep = append(keep, line)
	}
	if !found {
		return fmt.Errorf("%s is not a host", alias)
	}
	return os.WriteFile(hostsFile(), []byte(strings.Join(keep, "\n")+"\n"), 0o644)
}

// Call runs a request on the host: in this process for the local host, as
// `factory _host` over ssh for any other, with the request on stdin so
// nothing in it ever meets a remote shell's quoting.
func (h Host) Call(req Request) (Response, error) {
	if h.Local() {
		resp := Handle(req)
		return resp, respErr(resp)
	}
	cmd := exec.Command("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=8", h.SSH,
		`exec "$SHELL" -lc 'factory _host'`)
	body, _ := json.Marshal(req)
	cmd.Stdin = bytes.NewReader(body)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var resp Response
	if jerr := json.Unmarshal(out, &resp); jerr != nil {
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(msg, "unknown argument") || strings.Contains(msg, "command not found") {
			return resp, fmt.Errorf("%s: its factory does not speak this version; install this build there", h.Name)
		}
		if err != nil {
			return resp, fmt.Errorf("%s: %v: %s", h.Name, err, msg)
		}
		return resp, fmt.Errorf("%s: unreadable answer: %s", h.Name, clip(string(out), 200))
	}
	return resp, respErr(resp)
}

func respErr(resp Response) error {
	if resp.Error == "" {
		return nil
	}
	if resp.NotFound {
		return ErrNoSession
	}
	return errors.New(resp.Error)
}

// ServeStdin is `factory _host`: one request on stdin, one response on
// stdout.
func ServeStdin() error {
	var req Request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(Handle(req))
}

// Each asks every host at once and returns the answers in host order.
func Each(hosts []Host, req Request) ([]Response, []error) {
	resps := make([]Response, len(hosts))
	errs := make([]error, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func(i int, h Host) {
			defer wg.Done()
			resps[i], errs[i] = h.Call(req)
		}(i, h)
	}
	wg.Wait()
	return resps, errs
}

// Find locates a session on whichever host has it.
func Find(prefix string) (Host, error) {
	hosts, err := Hosts()
	if err != nil {
		return Host{}, err
	}
	resps, errs := Each(hosts, Request{Op: "list"})
	var found []Host
	var unreachable []string
	for i, h := range hosts {
		if errs[i] != nil {
			unreachable = append(unreachable, h.Name)
			continue
		}
		for _, s := range resps[i].Sessions {
			if strings.HasPrefix(s.ID, prefix) {
				found = append(found, h)
				break
			}
		}
	}
	switch {
	case len(found) == 1:
		return found[0], nil
	case len(found) > 1:
		return Host{}, fmt.Errorf("%q matches sessions on more than one host; use more of the id", prefix)
	case len(unreachable) > 0:
		return Host{}, fmt.Errorf("no session %q on a reachable host (unreachable: %s)", prefix, strings.Join(unreachable, ", "))
	default:
		return Host{}, fmt.Errorf("no session %q", prefix)
	}
}

// AttachTmux replaces this process with a tmux client on the host.
func AttachTmux(h Host, name string) error {
	if h.Local() {
		if os.Getenv("TMUX") != "" {
			return exec.Command("tmux", "switch-client", "-t", "="+name).Run()
		}
		return execvp("tmux", "attach", "-t", "="+name)
	}
	return execvp("ssh", "-t", h.SSH, `exec "$SHELL" -lc 'tmux attach -t =`+name+`'`)
}

func execvp(name string, args ...string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return err
	}
	return syscall.Exec(path, append([]string{name}, args...), os.Environ())
}
