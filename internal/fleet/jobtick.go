package fleet

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// What a tick does to the world, as variables so tests can stand in for a
// gaffer session and GitHub.
var (
	startGafferFn = startGaffer
	sendFn        = Send
	viewPRFn      = viewPR
)

// JobWake is one gaffer tick started or woke.
type JobWake struct {
	Job     string
	Gaffer  string
	Started bool // a new gaffer, rather than a follow-up to a live one
	Changes int
}

// tickJobs routes the tick's events to their jobs, reads each open job's pull
// requests, and settles what can be settled without a model: a part merged,
// a part now ready, a job done or past its ceiling. Only a job where
// something changed wakes its gaffer, and only its gaffer.
func tickJobs(events []Event) []JobWake {
	jobs, err := ListJobs()
	if err != nil || len(jobs) == 0 {
		return nil
	}
	changes := map[string][]string{} // job → lines for its gaffer
	heard := map[string]bool{}       // the operator said something
	for _, e := range events {
		if e.Job == "" {
			continue
		}
		if e.Gaffer {
			switch e.Kind {
			case EvTurnDone:
				if e.Result != "" {
					appendJobLog(e.Job, "gaffer "+e.Session, e.Result)
				}
			case EvTurnFailed, EvDied:
				appendJobLog(e.Job, "tick", fmt.Sprintf("Gaffer %s %s; waking it to carry on.", e.Session, pastTense(e.Kind)))
				changes[e.Job] = append(changes[e.Job], fmt.Sprintf("- Your previous turn %s. Carry on from the job file.", pastTense(e.Kind)))
			}
			continue
		}
		switch e.Kind {
		case EvOperator:
			heard[e.Job] = true
			changes[e.Job] = append(changes[e.Job], "- The operator says: "+e.Result)
		case EvTurnDone, EvTurnFailed, EvDied, EvKilled:
			status := map[string]string{EvTurnDone: PartIdle, EvTurnFailed: PartFailed, EvDied: PartDied, EvKilled: PartKilled}[e.Kind]
			updateJobState(e.Job, func(st *JobState) error {
				if ps, ok := st.Parts[e.Part]; ok && ps.Session == e.Session && ps.Status != PartMerged {
					ps.Status = status
					st.Parts[e.Part] = ps
				}
				return nil
			})
			line := fmt.Sprintf("- Part %s (session %s) %s", e.Part, e.Session, describe(e))
			changes[e.Job] = append(changes[e.Job], line)
		}
	}

	var wakes []JobWake
	for _, j := range jobs {
		if j.State.Status != JobOpen && j.State.Status != JobWaiting {
			continue
		}
		lines := append(changes[j.ID], readPRs(j, heard)...)
		lines = append(lines, markReady(j)...)
		j, _ = LoadJob(j.ID) // parts may have merged above

		// Done is checked whenever something changed, and every five minutes
		// regardless: what a done-when check looks at (a merged PR elsewhere,
		// a file, a deploy) can change with no event here at all.
		if len(lines) > 0 || j.State.Gaffer == "" || time.Since(j.State.DoneChecked) > 5*time.Minute {
			updateJobState(j.ID, func(st *JobState) error { st.DoneChecked = time.Now().UTC(); return nil })
			if done, why := jobDone(j); done {
				SetJobStatus(j.ID, JobDone, why, "tick")
				continue
			}
		}
		if len(lines) == 0 && j.State.Gaffer != "" {
			continue // nothing changed: no model call
		}
		if why := pastCeiling(j); why != "" {
			SetJobStatus(j.ID, JobStopped, why+" Raise it with `factory job ceiling "+j.ID+"`.", "tick")
			continue
		}
		if heard[j.ID] && j.State.Status == JobWaiting {
			SetJobStatus(j.ID, JobOpen, "the operator answered", "tick")
			j, _ = LoadJob(j.ID)
		}
		if w, err := wakeGaffer(j, lines); err == nil {
			wakes = append(wakes, w)
		} else {
			appendJobLog(j.ID, "tick", "Could not wake the gaffer: "+err.Error())
		}
	}
	return wakes
}

func pastTense(kind string) string {
	switch kind {
	case EvTurnFailed:
		return "failed"
	case EvDied:
		return "died"
	case EvKilled:
		return "was killed"
	}
	return "ended"
}

func describe(e Event) string {
	switch e.Kind {
	case EvTurnDone:
		return fmt.Sprintf("ended turn %d: %s", e.Turn, clip(strings.Join(strings.Fields(e.Result), " "), 600))
	case EvTurnFailed:
		return fmt.Sprintf("failed turn %d: %s", e.Turn, clip(strings.Join(strings.Fields(e.Result), " "), 600))
	}
	return pastTense(e.Kind) + "."
}

// readPRs reads the pull request of every part with a session and reports
// what changed since the last tick: state, checks, review, and comments from
// anyone but this host's own login. A merge or close settles the part.
func readPRs(j Job, heard map[string]bool) []string {
	var lines []string
	me := ghLogin()
	names := make([]string, 0, len(j.State.Parts))
	for n := range j.State.Parts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		ps := j.State.Parts[name]
		if ps.Session == "" || ps.Status == PartMerged || ps.Status == PartClosed {
			continue
		}
		m, err := loadMeta(ps.Session)
		if err != nil || m.Repo == "" {
			continue
		}
		var v *prView
		for _, branch := range prBranches(m) {
			if v = viewPRFn(m.Repo, branch); v != nil {
				break
			}
		}
		if v == nil || v.digest() == ps.Seen {
			continue
		}
		line := fmt.Sprintf("- Part %s: pull request #%d is %s; checks %s", name, v.Number, strings.ToLower(v.State), v.checkSummary())
		if v.Review != "" {
			line += "; review " + strings.ToLower(v.Review)
		}
		lines = append(lines, line+". "+v.URL)
		for _, c := range v.Comments[min(ps.Heard, len(v.Comments)):] {
			if c.Author.Login == me || strings.TrimSpace(c.Body) == "" {
				continue
			}
			heard[j.ID] = true
			lines = append(lines, fmt.Sprintf("  - @%s: %s", c.Author.Login, clip(strings.Join(strings.Fields(c.Body), " "), 600)))
		}
		updateJobState(j.ID, func(st *JobState) error {
			p := st.Parts[name]
			p.PR = &PR{Number: v.Number, URL: v.URL, State: v.State}
			p.Checks, p.Review, p.Seen, p.Heard = v.checkSummary(), v.Review, v.digest(), len(v.Comments)
			switch v.State {
			case "MERGED":
				p.Status = PartMerged
			case "CLOSED":
				p.Status = PartClosed
			}
			st.Parts[name] = p
			return nil
		})
	}
	return lines
}

// markReady moves waiting parts whose after lists have all merged to ready.
func markReady(j Job) []string {
	var lines []string
	updateJobState(j.ID, func(st *JobState) error {
		for _, p := range j.Parts {
			if st.Parts[p.Name].Status == PartWaiting && partReadiness(j.JobSpec, *st, p) == PartReady {
				ps := st.Parts[p.Name]
				ps.Status = PartReady
				st.Parts[p.Name] = ps
				lines = append(lines, fmt.Sprintf("- Part %s is ready to start: everything in its after list has merged.", p.Name))
			}
		}
		return nil
	})
	return lines
}

// jobDone is the done-when check when the job has one, else every part
// merged (and at least one part).
func jobDone(j Job) (bool, string) {
	if j.DoneWhen != "" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "sh", "-c", j.DoneWhen)
		cmd.Dir = jobDir(j.ID)
		if cmd.Run() == nil {
			return true, "done-when passed: " + j.DoneWhen
		}
		return false, ""
	}
	if len(j.Parts) == 0 {
		return false, ""
	}
	for _, p := range j.Parts {
		if j.State.Parts[p.Name].Status != PartMerged {
			return false, ""
		}
	}
	return true, "every part's pull request has merged."
}

// pastCeiling says why a job has run out of room, or "".
func pastCeiling(j Job) string {
	if j.State.Wakes >= j.Ceiling.Wakes {
		return fmt.Sprintf("Past its ceiling: %d wakes.", j.Ceiling.Wakes)
	}
	if time.Since(j.Created) > time.Duration(j.Ceiling.Days)*24*time.Hour {
		return fmt.Sprintf("Past its ceiling: %d days.", j.Ceiling.Days)
	}
	return ""
}

// wakeGaffer hands the gaffer what changed: a follow-up to its live session,
// or a new gaffer when there is none or the operator killed it.
func wakeGaffer(j Job, lines []string) (JobWake, error) {
	w := JobWake{Job: j.ID, Changes: len(lines)}
	text := fmt.Sprintf("Wake %d for job %s at %s. What changed:\n%s\n", j.State.Wakes+1, j.ID,
		time.Now().UTC().Format("2006-01-02 15:04Z"), strings.Join(lines, "\n"))
	if len(lines) == 0 {
		text = ""
	}
	gaffer := j.State.Gaffer
	if gaffer != "" {
		if _, err := loadMeta(gaffer); err != nil || liveState(gaffer).Status == Killed {
			gaffer = ""
		}
	}
	if gaffer != "" {
		if _, err := sendFn(gaffer, text); err != nil {
			return w, err
		}
		w.Gaffer = gaffer
	} else {
		m, err := startGafferFn(j, strings.Join(lines, "\n"))
		if err != nil {
			return w, err
		}
		w.Gaffer, w.Started = m.ID, true
	}
	updateJobState(j.ID, func(st *JobState) error {
		st.Gaffer = w.Gaffer
		st.Wakes++
		return nil
	})
	verb := "Woke gaffer " + w.Gaffer
	if w.Started {
		verb = "Started gaffer " + w.Gaffer
	}
	appendJobLog(j.ID, "tick", fmt.Sprintf("%s (wake %d) with %d changes.", verb, j.State.Wakes+1, len(lines)))
	return w, nil
}
