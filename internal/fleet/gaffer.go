package fleet

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// A gaffer is a job's coordinator: an ordinary session with no repo, working
// in the job's directory, started and woken by tick. It starts parts, reads
// what they did, sends them follow-ups, merges a part's pull request once its
// CI is green and its acceptance checks pass, and tells the operator when it
// needs them. It never approves, and never publishes a release. The job file is the
// truth and the gaffer's context is a cache of it, so a lost gaffer is
// replaced by a new one that reads the file.

// gafferBrief is a gaffer's first turn.
func gafferBrief(j Job, extra, events string) string {
	who := ghLogin()
	if who == "" {
		who = "this host's gh login"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `You are the gaffer for job %s, on %s, acting as %s. You coordinate this job from its ask to its last merged pull request. Nobody is watching you live.

The job is in %s:
- job.md: the ask, its parts (each a repo and a task, run after the parts in its "after" list have merged), done-when, and ceiling. It is the source of truth.
- state.json: each part's session, status and pull request, and how many times you have been woken.
- log.md: what happened so far.

You are woken when something changes: a part's session ends a turn, fails or dies, its pull request changes (checks, review, comments, merge), or the operator says something. Each wake tells you what changed. Between wakes you are not running, so never wait: do what the change calls for, then end your turn.

Your tools are the factory CLI, on this host:
- factory job part-add %[1]s NAME OWNER/REPO "TASK" [--after a,b]   split the work (only if job.md has no parts, or the split needs another)
- factory run --job %[1]s --part NAME                                  start a part; refused until everything in its after list has merged
- factory peek ID, factory send ID "MESSAGE", factory kill ID          read, steer or stop a part's session
- factory job wait %[1]s "WHAT YOU NEED"                               when only the operator can unblock it; also say it on the pull request
- factory job open %[1]s                                               when it no longer waits
- factory job progress %[1]s "WHAT HAPPENED"                           post a milestone to the job's Slack thread
- gh pr view / gh pr checks / gh pr comment                            read and answer on pull requests

Rules:
- When configured, the factory foreman is your floor-wide supervisor. Follow its job messages within the operator's existing authorization. It coordinates cross-job dependencies and recovery; you retain ownership of your workers. Send cross-job blockers to factory foreman say so it can track them.
- The factory merges its own work into the next release, which is the repository's default branch unless the job names another. When a part's pull request has its CI workflow green (check the workflow by name with gh pr checks, not only that the pull request is mergeable) and the part's acceptance checks pass, merge it with gh pr merge --merge, unless its session already has. Do not wait for the operator to review it: they review what landed, and own releases.
- Never approve or close a pull request, and never publish, undraft or tag a release.
- Never name a branch in a part's task. Every part starts on a branch of its own, and its pull request is looked for there.
- After you start a part or send it a follow-up, end your turn. Never sleep, poll, or peek in a loop to see how it is going: you are woken when it ends its turn.
- Never edit job.md, state.json or log.md yourself, and never touch whatever the done-when check looks at. Record through factory job commands; your end-of-turn paragraph is logged for you.
- Start a part only when it is ready. Tick marks a part merged when its pull request merges, and tells you.
- When a part's pull request fails its checks or gets review comments, send that part's session a follow-up that says exactly what to fix. Do not fix it yourself.
- A part whose session died or failed can be resumed with factory send ID "carry on", or restarted with factory run.
- Post factory job progress at milestones only: the job split into parts, a part's pull request merged, a part restarted after failing, or a decision that changes the plan. One sentence, with the pull request number when there is one. Never for routine turns, and not for waiting or done, which tick posts itself. It notifies nobody; it keeps the job's thread in #factory current.
- Tick decides when the job is done (done-when, or every part merged). You do not.
- End every turn with one short paragraph: what you did, and what you are waiting on. It is written to log.md.
`, j.ID, hostname(), who, jobDir(j.ID))
	if extra != "" {
		b.WriteString(extra)
	}
	b.WriteString("\nThe ask:\n")
	b.WriteString(j.Ask)
	b.WriteString("\n")
	if len(j.Parts) == 0 {
		b.WriteString("\njob.md has no parts yet. Split the ask into parts with factory job part-add, then start the ones that are ready.\n")
	} else {
		b.WriteString("\nParts:\n")
		for _, p := range j.Parts {
			after := ""
			if len(p.After) > 0 {
				after = " (after " + strings.Join(p.After, ", ") + ")"
			}
			fmt.Fprintf(&b, "- %s: %s%s: %s — %s\n", p.Name, p.Repo, after, j.State.Parts[p.Name].Status, firstLine(p.Task))
		}
	}
	if events != "" {
		b.WriteString("\nWhat changed:\n")
		b.WriteString(events)
	}
	return b.String()
}

// startGaffer starts a job's coordinator: a session with no repo, working in
// the job's directory.
func startGaffer(j Job, events string) (Meta, error) {
	harness := firstNonEmpty(os.Getenv("FACTORY_GAFFER_HARNESS"), "claude")
	if _, err := exec.LookPath(harness); err != nil {
		return Meta{}, fmt.Errorf("%s is not installed on %s", harness, hostname())
	}
	id := NewID()
	if err := os.MkdirAll(sessionDir(id), 0o755); err != nil {
		return Meta{}, err
	}
	m := Meta{
		ID:        id,
		Host:      hostname(),
		Worktree:  jobDir(j.ID),
		Harness:   harness,
		Model:     os.Getenv("FACTORY_GAFFER_MODEL"),
		Task:      "gaffer for job " + j.ID,
		Login:     ghLogin(),
		CreatedAt: time.Now().UTC(),
		Kind:      KindGaffer,
		Job:       j.ID,
	}
	if err := writeJSON(filepath.Join(sessionDir(id), "meta.json"), m); err != nil {
		return Meta{}, err
	}
	prompt := gafferBrief(j, extraBrief(m), events)
	if err := os.WriteFile(filepath.Join(sessionDir(id), "prompt.md"), []byte(prompt), 0o644); err != nil {
		return Meta{}, err
	}
	if err := saveState(id, State{Status: Running}); err != nil {
		return Meta{}, err
	}
	if err := startRunner(m); err != nil {
		return m, err
	}
	emitFor(m, EvStarted, State{})
	return m, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// prView is what tick reads about a part's pull request.
type prView struct {
	Number   int         `json:"number"`
	URL      string      `json:"url"`
	State    string      `json:"state"`
	Review   string      `json:"reviewDecision"`
	Checks   []check     `json:"-"`
	Comments []prComment `json:"-"`
}

type check struct {
	Name, Context, Status, Conclusion, State string
	WorkflowName                             string
	StartedAt                                string // RFC 3339, so it sorts as text
}

type prComment struct {
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Body  string `json:"body"`
	State string `json:"state"` // on reviews: APPROVED, CHANGES_REQUESTED, COMMENTED
}

// viewPR reads a branch's pull request, or nil when it has none.
func viewPR(repo, branch string) *prView {
	out, err := exec.Command("gh", "pr", "view", branch, "-R", repo,
		"--json", "number,url,state,reviewDecision,statusCheckRollup,comments,reviews").Output()
	if err != nil {
		return nil
	}
	var raw struct {
		prView
		Rollup   []check     `json:"statusCheckRollup"`
		Comments []prComment `json:"comments"`
		Reviews  []prComment `json:"reviews"`
	}
	if json.Unmarshal(out, &raw) != nil {
		return nil
	}
	v := raw.prView
	v.Checks = raw.Rollup
	v.Comments = append(raw.Comments, raw.Reviews...)
	return &v
}

// checkSummary is "passing", "pending", or "failing: name, name", read from
// the latest run of each check.
func (v *prView) checkSummary() string {
	var failing []string
	pending := false
	for _, c := range latestChecks(v.Checks) {
		name := firstNonEmpty(c.Name, c.Context)
		switch {
		case c.Conclusion == "FAILURE" || c.Conclusion == "CANCELLED" || c.Conclusion == "TIMED_OUT" ||
			c.Conclusion == "ACTION_REQUIRED" || c.State == "FAILURE" || c.State == "ERROR":
			failing = append(failing, name)
		case (c.Status != "" && c.Status != "COMPLETED") || c.State == "PENDING" || c.State == "EXPECTED":
			pending = true
		}
	}
	switch {
	case len(failing) > 0:
		return "failing: " + strings.Join(failing, ", ")
	case pending:
		return "pending"
	case len(v.Checks) == 0:
		return "none"
	default:
		return "passing"
	}
}

// latestChecks keeps one run of each check, the latest. The rollup lists every
// run on the head commit, so a run cancelled because a newer one superseded
// it stays there beside the newer one's success, and would read as failing.
// A run still going is always the latest: a superseded one is cancelled.
func latestChecks(checks []check) []check {
	pending := func(c check) bool {
		return (c.Status != "" && c.Status != "COMPLETED") || c.State == "PENDING" || c.State == "EXPECTED"
	}
	newer := func(a, b check) bool { // a replaces b
		if pending(a) != pending(b) {
			return pending(a)
		}
		return a.StartedAt >= b.StartedAt
	}
	index := map[string]int{}
	var out []check
	for _, c := range checks {
		key := c.WorkflowName + "\x00" + firstNonEmpty(c.Name, c.Context)
		i, seen := index[key]
		switch {
		case !seen:
			index[key] = len(out)
			out = append(out, c)
		case newer(c, out[i]):
			out[i] = c
		}
	}
	return out
}

// digest is what makes a pull request "changed" from one tick to the next.
func (v *prView) digest() string {
	return fmt.Sprintf("%s|%s|%s|%d", v.State, v.Review, v.checkSummary(), len(v.Comments))
}
