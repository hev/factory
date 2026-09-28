package fleet

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Two more seams, the same shape as factory-brief: an executable on the job
// owner's PATH, satisfiable by a shell script, and a build without one works
// the same minus the feature.
//
//	factory-intake                    prints new work, one job spec per line as JSON
//	factory-intake filed SOURCE JOB   the job for SOURCE was filed as JOB
//	factory-notify KIND JOB           tells the operator; KIND is waiting, done,
//	                                  stopped or stuck, and the message is on stdin

// runSeam runs a seam if it is on PATH. ok is false when it isn't.
func runSeam(name string, args []string, stdin string, env []string, timeout time.Duration, logPath string) (out string, ok bool, err error) {
	path, lerr := exec.LookPath(name)
	if lerr != nil {
		return "", false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(), env...)
	if f, ferr := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); ferr == nil {
		defer f.Close()
		cmd.Stderr = f
	}
	b, err := cmd.Output()
	return string(b), true, err
}

// runIntake files whatever factory-intake prints. A spec needs a source (the
// issue it came from, say), and a source with a job already open or waiting
// is not filed twice: that job's id is acknowledged again instead, so an
// acknowledgement lost to a crash is repaired on the next tick.
func runIntake() []string {
	logPath := filepath.Join(Home(), "intake.log")
	out, ok, err := runSeam("factory-intake", nil, "", nil, time.Minute, logPath)
	if !ok {
		return nil
	}
	if err != nil {
		intakeLog("factory-intake failed: %v", err)
	}
	jobs, _ := ListJobs()
	live := map[string]string{} // source → id of its open or waiting job
	for _, j := range jobs {
		if j.Source != "" && (j.State.Status == JobOpen || j.State.Status == JobWaiting) {
			live[j.Source] = j.ID
		}
	}
	var filed []string
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var s JobSpec
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			intakeLog("not a job spec: %v: %s", err, clip(line, 200))
			continue
		}
		if s.Source == "" {
			intakeLog("no source, not filed: %s", clip(firstLine(s.Ask), 200))
			continue
		}
		id, seen := live[s.Source]
		if !seen {
			j, err := AddJob(s)
			if err != nil {
				intakeLog("%s not filed: %v", s.Source, err)
				continue
			}
			id, live[s.Source] = j.ID, j.ID
			filed = append(filed, id)
		}
		if _, _, err := runSeam("factory-intake", []string{"filed", s.Source, id}, "", nil, time.Minute, logPath); err != nil {
			intakeLog("factory-intake filed %s %s failed: %v", s.Source, id, err)
		}
	}
	return filed
}

func intakeLog(format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(Home(), "intake.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format("2006-01-02T15:04:05Z"), fmt.Sprintf(format, args...))
}

// Alert kinds: the job statuses the operator hears about, and stuck, for a
// job whose gaffer could not be woken.
const (
	AlertWaiting = JobWaiting
	AlertDone    = JobDone
	AlertStopped = JobStopped
	AlertStuck   = "stuck"
)

// notify hands an alert to factory-notify. What the job and its ask are go in
// the environment, so a one-line script can say something useful.
func notify(kind string, j Job, message string) {
	env := []string{"FACTORY_JOB=" + j.ID, "FACTORY_JOB_SOURCE=" + j.Source,
		"FACTORY_JOB_ASK=" + firstLine(j.Ask), "FACTORY_HOST=" + hostname()}
	_, ok, err := runSeam("factory-notify", []string{kind, j.ID}, message, env, 10*time.Second, filepath.Join(jobDir(j.ID), "notify.log"))
	if ok && err != nil {
		appendJobLog(j.ID, "tick", fmt.Sprintf("factory-notify %s failed: %v", kind, err))
	}
}
