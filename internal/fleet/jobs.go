package fleet

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// A job is a piece of work the always-on host owns from the ask to its last
// pull request: one directory, ~/.factory/jobs/<id>/.
//
//	job.md      the spec as TOML front matter (+++), then the ask in prose
//	state.json  where it has got to: parts → sessions and PRs, status, wakes
//	log.md      what happened on each wake, appended
//
// job.md is the source of truth. Whatever coordinates a job (its gaffer)
// holds a cache of it, so the coordinator can be replaced at any time.

// Job statuses.
const (
	JobOpen    = "open"    // work to do or in flight
	JobWaiting = "waiting" // needs the operator; WaitingOnYou says what for
	JobDone    = "done"    // done-when passed, or every part's PR merged
	JobStopped = "stopped" // past its ceiling, or stopped by hand
)

// Part is one session's worth of a job. It runs once every part named in
// After has merged.
type Part struct {
	Name  string   `toml:"name" json:"name"`
	Repo  string   `toml:"repo" json:"repo"`
	Task  string   `toml:"task" json:"task"`
	After []string `toml:"after,omitempty" json:"after,omitempty"`
	Line  string   `toml:"line,omitempty" json:"line,omitempty"`
}

// Ceiling bounds a job. Wakes are exact; days are wall-clock from creation.
type Ceiling struct {
	Wakes int `toml:"wakes" json:"wakes"`
	Days  int `toml:"days" json:"days"`
}

// DefaultCeiling applies to any bound a job leaves at zero.
var DefaultCeiling = Ceiling{Wakes: 50, Days: 7}

// JobSpec is what was asked: job.md.
type JobSpec struct {
	ID       string    `toml:"id" json:"id"`
	Line     string    `toml:"line,omitempty" json:"line,omitempty"`
	Source   string    `toml:"source,omitempty" json:"source,omitempty"` // "reception", "linear:LYR-112", …
	Created  time.Time `toml:"created" json:"created"`
	DoneWhen string    `toml:"done_when,omitempty" json:"done_when,omitempty"`
	Ceiling  Ceiling   `toml:"ceiling" json:"ceiling"`
	Parts    []Part    `toml:"parts,omitempty" json:"parts,omitempty"`
	Ask      string    `toml:"-" json:"ask"`
}

// PartState is where one part has got to.
type PartState struct {
	Session string `json:"session,omitempty"`
	Host    string `json:"host,omitempty"`
	Status  string `json:"status,omitempty"` // see the Part statuses
	PR      *PR    `json:"pr,omitempty"`
	Checks  string `json:"checks,omitempty"` // passing, pending, failing: …
	Review  string `json:"review,omitempty"` // APPROVED, CHANGES_REQUESTED, …
	Seen    string `json:"seen,omitempty"`   // the PR digest tick last reported
	Heard   int    `json:"heard,omitempty"`  // comments and reviews tick has read
}

// JobState is where a job has got to: state.json.
type JobState struct {
	Status       string               `json:"status"`
	Gaffer       string               `json:"gaffer,omitempty"` // the coordinating session's id
	Parts        map[string]PartState `json:"parts,omitempty"`
	WaitingOnYou string               `json:"waiting_on_you,omitempty"`
	BlockedOn    string               `json:"blocked_on,omitempty"`
	Wakes        int                  `json:"wakes"`
	DoneChecked  time.Time            `json:"done_checked,omitempty"` // last run of the done-when check
	Stuck        bool                 `json:"stuck,omitempty"`        // the gaffer could not be woken, and the operator was told
	UpdatedAt    time.Time            `json:"updated_at"`
}

// Job is one row: the spec and its state.
type Job struct {
	JobSpec
	State JobState `json:"state"`
}

func jobsDir() string            { return filepath.Join(Home(), "jobs") }
func jobDir(id string) string    { return filepath.Join(jobsDir(), id) }
func jobMDPath(id string) string { return filepath.Join(jobDir(id), "job.md") }

// Validate checks a spec before it is filed: parts named once, repos in
// OWNER/REPO form, After naming parts that exist, and no cycle among them.
func (s JobSpec) Validate() error {
	if strings.TrimSpace(s.Ask) == "" {
		return errors.New("a job needs an ask")
	}
	names := map[string]bool{}
	for _, p := range s.Parts {
		if p.Name == "" || strings.ContainsAny(p.Name, " \t\n/") {
			return fmt.Errorf("part name %q: one word, no slashes", p.Name)
		}
		if names[p.Name] {
			return fmt.Errorf("part %q is named twice", p.Name)
		}
		names[p.Name] = true
		if !repoPattern.MatchString(p.Repo) {
			return fmt.Errorf("part %q: repo must be OWNER/REPO, got %q", p.Name, p.Repo)
		}
		if strings.TrimSpace(p.Task) == "" {
			return fmt.Errorf("part %q has no task", p.Name)
		}
	}
	after := map[string][]string{}
	for _, p := range s.Parts {
		for _, a := range p.After {
			if !names[a] {
				return fmt.Errorf("part %q runs after %q, which is not a part", p.Name, a)
			}
		}
		after[p.Name] = p.After
	}
	// Depth-first: a part reached again on its own path is a cycle.
	state := map[string]int{} // 0 unseen, 1 on the path, 2 done
	var visit func(string) error
	visit = func(n string) error {
		switch state[n] {
		case 1:
			return fmt.Errorf("parts run after each other in a cycle through %q", n)
		case 2:
			return nil
		}
		state[n] = 1
		for _, a := range after[n] {
			if err := visit(a); err != nil {
				return err
			}
		}
		state[n] = 2
		return nil
	}
	for _, p := range s.Parts {
		if err := visit(p.Name); err != nil {
			return err
		}
	}
	return nil
}

// render writes job.md: TOML front matter, then the ask.
func (s JobSpec) render() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("+++\n")
	if err := toml.NewEncoder(&b).Encode(s); err != nil {
		return nil, err
	}
	b.WriteString("+++\n\n")
	b.WriteString(strings.TrimSpace(s.Ask))
	b.WriteString("\n")
	return b.Bytes(), nil
}

// parseJobMD reads job.md back.
func parseJobMD(data []byte) (JobSpec, error) {
	var s JobSpec
	text := string(data)
	rest, ok := strings.CutPrefix(text, "+++\n")
	if !ok {
		return s, errors.New("job.md has no +++ front matter")
	}
	front, body, ok := strings.Cut(rest, "\n+++\n")
	if !ok {
		return s, errors.New("job.md front matter is not closed")
	}
	if _, err := toml.Decode(front, &s); err != nil {
		return s, fmt.Errorf("job.md: %w", err)
	}
	s.Ask = strings.TrimSpace(body)
	return s, nil
}

// AddJob files a job on this host.
func AddJob(s JobSpec) (Job, error) {
	s.ID = NewID()
	s.Created = time.Now().UTC().Truncate(time.Second)
	if s.Ceiling.Wakes <= 0 {
		s.Ceiling.Wakes = DefaultCeiling.Wakes
	}
	if s.Ceiling.Days <= 0 {
		s.Ceiling.Days = DefaultCeiling.Days
	}
	if err := s.Validate(); err != nil {
		return Job{}, err
	}
	data, err := s.render()
	if err != nil {
		return Job{}, err
	}
	if err := os.MkdirAll(jobDir(s.ID), 0o755); err != nil {
		return Job{}, err
	}
	if err := os.WriteFile(jobMDPath(s.ID), data, 0o644); err != nil {
		return Job{}, err
	}
	st := JobState{Status: JobOpen, Parts: map[string]PartState{}, UpdatedAt: time.Now().UTC()}
	for _, p := range s.Parts {
		status := PartReady // nothing to wait for; the gaffer can start it
		if len(p.After) > 0 {
			status = PartWaiting
		}
		st.Parts[p.Name] = PartState{Status: status}
	}
	if err := writeJSON(filepath.Join(jobDir(s.ID), "state.json"), st); err != nil {
		return Job{}, err
	}
	source := s.Source
	if source == "" {
		source = "reception"
	}
	appendJobLog(s.ID, source, "Filed: "+firstLine(s.Ask))
	return Job{JobSpec: s, State: st}, nil
}

// LoadJob reads a job by id or unique id prefix.
func LoadJob(prefix string) (Job, error) {
	id, err := resolveJob(prefix)
	if err != nil {
		return Job{}, err
	}
	data, err := os.ReadFile(jobMDPath(id))
	if err != nil {
		return Job{}, err
	}
	s, err := parseJobMD(data)
	if err != nil {
		return Job{}, err
	}
	var st JobState
	_ = readJSON(filepath.Join(jobDir(id), "state.json"), &st)
	return Job{JobSpec: s, State: st}, nil
}

// ErrNoJob is a job id this host has never heard of.
var ErrNoJob = errors.New("no such job")

func resolveJob(prefix string) (string, error) {
	entries, err := os.ReadDir(jobsDir())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var hits []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			if e.Name() == prefix {
				return prefix, nil
			}
			hits = append(hits, e.Name())
		}
	}
	switch len(hits) {
	case 0:
		return "", ErrNoJob
	case 1:
		return hits[0], nil
	default:
		sort.Strings(hits)
		return "", fmt.Errorf("%q matches jobs %s", prefix, strings.Join(hits, ", "))
	}
}

// ListJobs is every job on this host, newest first.
func ListJobs() ([]Job, error) {
	entries, err := os.ReadDir(jobsDir())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var out []Job
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if j, err := LoadJob(e.Name()); err == nil {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Created.After(out[k].Created) })
	return out, nil
}

// JobLog is the last n entries of a job's log.md, each starting at its
// "## <time> · <who>" heading.
func JobLog(prefix string, n int) (string, error) {
	id, err := resolveJob(prefix)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(jobDir(id), "log.md"))
	if err != nil {
		return "", nil
	}
	var entries []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "## ") || len(entries) == 0 {
			entries = append(entries, line)
			continue
		}
		entries[len(entries)-1] += "\n" + line
	}
	if n > 0 && len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	return strings.TrimSpace(strings.Join(entries, "\n")), nil
}

// appendJobLog adds one entry to log.md under a UTC timestamp and who wrote it.
func appendJobLog(id, who, text string) {
	f, err := os.OpenFile(filepath.Join(jobDir(id), "log.md"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "## %s · %s\n\n%s\n\n", time.Now().UTC().Format("2006-01-02 15:04:05Z"), who, strings.TrimSpace(text))
}

// updateJobState is a read-modify-write of state.json under the job's lock,
// so tick, the gaffer's commands and reception never overwrite each other.
func updateJobState(id string, fn func(*JobState) error) (JobState, error) {
	unlock, err := flock(filepath.Join(jobDir(id), "state.lock"), true)
	if err != nil {
		return JobState{}, err
	}
	defer unlock()
	var st JobState
	_ = readJSON(filepath.Join(jobDir(id), "state.json"), &st)
	if st.Parts == nil {
		st.Parts = map[string]PartState{}
	}
	if err := fn(&st); err != nil {
		return st, err
	}
	st.UpdatedAt = time.Now().UTC()
	return st, writeJSON(filepath.Join(jobDir(id), "state.json"), st)
}

// saveSpec rewrites job.md, validated first, under the job's spec lock.
func saveSpec(s JobSpec) error {
	if err := s.Validate(); err != nil {
		return err
	}
	data, err := s.render()
	if err != nil {
		return err
	}
	tmp := jobMDPath(s.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, jobMDPath(s.ID))
}

// AddPart adds a part to a job: how a gaffer records the split it chose.
func AddPart(jobID string, p Part, who string) (Job, error) {
	unlock, err := flock(filepath.Join(jobDir(jobID), "spec.lock"), true)
	if err != nil {
		return Job{}, err
	}
	defer unlock()
	j, err := LoadJob(jobID)
	if err != nil {
		return Job{}, err
	}
	j.Parts = append(j.Parts, p)
	if err := saveSpec(j.JobSpec); err != nil {
		return Job{}, err
	}
	st, err := updateJobState(j.ID, func(st *JobState) error {
		st.Parts[p.Name] = PartState{Status: partReadiness(j.JobSpec, *st, p)}
		return nil
	})
	if err != nil {
		return Job{}, err
	}
	appendJobLog(j.ID, who, fmt.Sprintf("Added part %s (%s): %s", p.Name, p.Repo, firstLine(p.Task)))
	j.State = st
	return j, nil
}

// partReadiness is "ready" when everything in the part's after list has
// merged, else "waiting".
func partReadiness(s JobSpec, st JobState, p Part) string {
	for _, a := range p.After {
		if st.Parts[a].Status != PartMerged {
			return PartWaiting
		}
	}
	return PartReady
}

// Part statuses.
const (
	PartReady   = "ready"   // may start
	PartWaiting = "waiting" // something in its after list has not merged
	PartRunning = "running" // its session is in a turn
	PartIdle    = "idle"    // its session ended a turn; see its PR
	PartFailed  = "failed"  // its session's last turn failed
	PartDied    = "died"    // its session died
	PartKilled  = "killed"  // its session was stopped
	PartMerged  = "merged"  // its PR merged
	PartClosed  = "closed"  // its PR closed without merging
)

// StartPart starts a job's part as a session, refusing while anything in its
// after list has not merged or while it is already running. REPO and TASK
// default to the part's own; a task given here replaces it for this run.
func StartPart(req StartRequest) (Meta, error) {
	j, err := LoadJob(req.Job)
	if err != nil {
		return Meta{}, err
	}
	var part *Part
	for i := range j.Parts {
		if j.Parts[i].Name == req.Part {
			part = &j.Parts[i]
		}
	}
	if part == nil {
		return Meta{}, fmt.Errorf("job %s has no part %q", j.ID, req.Part)
	}
	var m Meta
	_, err = updateJobState(j.ID, func(st *JobState) error {
		ps := st.Parts[part.Name]
		if ps.Status == PartRunning {
			return fmt.Errorf("part %s is already running as session %s", part.Name, ps.Session)
		}
		if ps.Status == PartMerged {
			return fmt.Errorf("part %s has merged", part.Name)
		}
		for _, a := range part.After {
			if st.Parts[a].Status != PartMerged {
				return fmt.Errorf("part %s runs after %s, which has not merged", part.Name, a)
			}
		}
		if req.Repo == "" {
			req.Repo = part.Repo
		}
		if strings.TrimSpace(req.Task) == "" {
			req.Task = part.Task
		}
		req.Job = j.ID
		var err error
		if m, err = startRepoSession(req); err != nil {
			return err
		}
		st.Parts[part.Name] = PartState{Session: m.ID, Host: m.Host, Status: PartRunning}
		return nil
	})
	if err != nil {
		return Meta{}, err
	}
	appendJobLog(j.ID, WhoAmI(), fmt.Sprintf("Started part %s as session %s on %s.", part.Name, m.ID, m.Host))
	return m, nil
}

// SetJobStatus moves a job by hand or by its gaffer: waiting (with what it
// needs from the operator), open, done or stopped.
func SetJobStatus(jobID, status, note, who string) (Job, error) {
	switch status {
	case JobOpen, JobWaiting, JobDone, JobStopped:
	default:
		return Job{}, fmt.Errorf("status %q", status)
	}
	id, err := resolveJob(jobID)
	if err != nil {
		return Job{}, err
	}
	if status == JobWaiting && strings.TrimSpace(note) == "" {
		return Job{}, errors.New("say what the job is waiting on")
	}
	_, err = updateJobState(id, func(st *JobState) error {
		st.Status = status
		st.WaitingOnYou = ""
		if status == JobWaiting {
			st.WaitingOnYou = note
		}
		return nil
	})
	if err != nil {
		return Job{}, err
	}
	msg := "Status: " + status
	if note != "" {
		msg += ". " + note
	}
	appendJobLog(id, who, msg)
	j, err := LoadJob(id)
	if err == nil && status != JobOpen {
		notify(status, j, note)
	}
	return j, err
}

// RaiseCeiling lifts a job's ceiling and reopens it if the ceiling stopped it.
func RaiseCeiling(jobID string, wakes, days int, who string) (Job, error) {
	unlock, err := flock(filepath.Join(jobDir(jobID), "spec.lock"), true)
	if err != nil {
		return Job{}, err
	}
	defer unlock()
	j, err := LoadJob(jobID)
	if err != nil {
		return Job{}, err
	}
	if wakes > 0 {
		j.Ceiling.Wakes = wakes
	}
	if days > 0 {
		j.Ceiling.Days = days
	}
	if err := saveSpec(j.JobSpec); err != nil {
		return Job{}, err
	}
	appendJobLog(j.ID, who, fmt.Sprintf("Ceiling raised to %d wakes / %d days.", j.Ceiling.Wakes, j.Ceiling.Days))
	if j.State.Status == JobStopped {
		return SetJobStatus(j.ID, JobOpen, "reopened under the new ceiling", who)
	}
	return LoadJob(j.ID)
}

// Say records something the operator told a job and emits it as an event, so
// the next tick wakes the gaffer with it.
func Say(jobID, message, who string) (Job, error) {
	j, err := LoadJob(jobID)
	if err != nil {
		return Job{}, err
	}
	if strings.TrimSpace(message) == "" {
		return Job{}, errors.New("say what?")
	}
	appendJobLog(j.ID, who, "Said: "+message)
	emit(Event{Kind: EvOperator, Job: j.ID, Session: who, Result: message})
	return j, nil
}

// AppendJobLog adds an entry to a job's log.md.
func AppendJobLog(jobID, who, text string) error {
	id, err := resolveJob(jobID)
	if err != nil {
		return err
	}
	appendJobLog(id, who, text)
	return nil
}

// WhoAmI names the writer of a log entry: the factory session running the
// command (a gaffer, usually), else the person.
func WhoAmI() string {
	if s := os.Getenv("FACTORY_SESSION"); s != "" {
		if m, err := loadMeta(s); err == nil && m.Kind == KindGaffer {
			return "gaffer " + s
		}
		return "session " + s
	}
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "reception"
}
