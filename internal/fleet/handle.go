package fleet

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// Request is one operation on this machine. Every command goes through
// Handle, so the CLI has one code path. The open build is one machine: a
// client that works on another (pro's, say) forwards whole commands through
// factory-remote, and never sends a Request anywhere.
type Request struct {
	Op      string        `json:"op"` // info, start, list, peek, send, kill, attach
	ID      string        `json:"id,omitempty"`
	Start   *StartRequest `json:"start,omitempty"`
	Lines   int           `json:"lines,omitempty"`
	Message string        `json:"message,omitempty"`
	Rm      bool          `json:"rm,omitempty"`
	PRs     bool          `json:"prs,omitempty"`
	Job     *JobSpec      `json:"job,omitempty"`
	Part    *Part         `json:"part,omitempty"`
	Status  string        `json:"status,omitempty"`
	Who     string        `json:"who,omitempty"`
	Wakes   int           `json:"wakes,omitempty"`
	Days    int           `json:"days,omitempty"`
}

// handleJobChange is every write to an existing job other than a part's start.
func handleJobChange(req Request) Response {
	var j Job
	var err error
	who := firstNonEmpty(req.Who, "reception")
	switch req.Op {
	case "job_part_add":
		if req.Part == nil {
			return Response{Error: "job_part_add: no part"}
		}
		j, err = AddPart(req.ID, *req.Part, who)
	case "job_status":
		j, err = SetJobStatus(req.ID, req.Status, req.Message, who)
	case "job_ceiling":
		j, err = RaiseCeiling(req.ID, req.Wakes, req.Days, who)
	case "job_say":
		j, err = Say(req.ID, req.Message, who)
	case "job_progress":
		j, err = Progress(req.ID, req.Message, who)
	case "job_log":
		if err = AppendJobLog(req.ID, who, req.Message); err == nil {
			j, err = LoadJob(req.ID)
		}
	}
	if err != nil {
		return Response{Error: err.Error(), NotFound: errors.Is(err, ErrNoJob)}
	}
	return Response{Job: &j}
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
	Job      *Job      `json:"job_row,omitempty"`
	Jobs     []Job     `json:"jobs,omitempty"`
	Log      string    `json:"log,omitempty"`
}

// Handle answers a request on this host.
func Handle(req Request) Response {
	var resp Response
	fail := func(err error) Response {
		return Response{Error: err.Error(), NotFound: errors.Is(err, ErrNoSession) || errors.Is(err, ErrNoJob)}
	}
	switch req.Op {
	case "job_add":
		if req.Job == nil {
			return fail(errors.New("job_add: no job"))
		}
		j, err := AddJob(*req.Job)
		if err != nil {
			return fail(err)
		}
		return Response{Job: &j}
	case "job_show":
		j, err := LoadJob(req.ID)
		if err != nil {
			return fail(err)
		}
		log, _ := JobLog(j.ID, req.Lines)
		return Response{Job: &j, Log: log}
	case "jobs":
		jobs, err := ListJobs()
		if err != nil {
			return fail(err)
		}
		return Response{Jobs: jobs}
	case "job_part_add", "job_status", "job_ceiling", "job_say", "job_progress", "job_log":
		return handleJobChange(req)
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

// Call answers a request on this machine, with its error as an error.
func Call(req Request) (Response, error) {
	resp := Handle(req)
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

// AttachTmux replaces this process with a tmux client on the session's tmux
// session, or switches to it from inside tmux.
func AttachTmux(name string) error {
	if os.Getenv("TMUX") != "" {
		return exec.Command("tmux", "switch-client", "-t", "="+name).Run()
	}
	return execvp("tmux", "attach", "-t", "="+name)
}

func execvp(name string, args ...string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return err
	}
	return syscall.Exec(path, append([]string{name}, args...), os.Environ())
}
