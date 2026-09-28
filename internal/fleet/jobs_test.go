package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJobSpecValidate(t *testing.T) {
	ok := JobSpec{Ask: "ship it", Parts: []Part{
		{Name: "schema", Repo: "hev/layer-pro", Task: "add the column"},
		{Name: "api", Repo: "hev/layer-pro", Task: "expose it", After: []string{"schema"}},
	}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]JobSpec{
		"no ask":     {},
		"dup part":   {Ask: "x", Parts: []Part{{Name: "a", Repo: "o/r", Task: "t"}, {Name: "a", Repo: "o/r", Task: "t"}}},
		"bad repo":   {Ask: "x", Parts: []Part{{Name: "a", Repo: "layer-pro", Task: "t"}}},
		"no task":    {Ask: "x", Parts: []Part{{Name: "a", Repo: "o/r"}}},
		"unknown":    {Ask: "x", Parts: []Part{{Name: "a", Repo: "o/r", Task: "t", After: []string{"b"}}}},
		"cycle":      {Ask: "x", Parts: []Part{{Name: "a", Repo: "o/r", Task: "t", After: []string{"b"}}, {Name: "b", Repo: "o/r", Task: "t", After: []string{"a"}}}},
		"self cycle": {Ask: "x", Parts: []Part{{Name: "a", Repo: "o/r", Task: "t", After: []string{"a"}}}},
		"bad name":   {Ask: "x", Parts: []Part{{Name: "a b", Repo: "o/r", Task: "t"}}},
	} {
		if bad.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestJobLifecycle(t *testing.T) {
	t.Setenv("FACTORY_HOME", t.TempDir())
	spec := JobSpec{Ask: "Add ordered scan.\n\nKeep COLLATE \"C\".", Line: "lyr", DoneWhen: "gh pr view 627 --json state",
		Parts: []Part{{Name: "scan", Repo: "hev/layer-pro", Task: "implement", After: nil}, {Name: "docs", Repo: "hev/lyr", Task: "document", After: []string{"scan"}}}}
	resp := Handle(Request{Op: "job_add", Job: &spec})
	if resp.Error != "" {
		t.Fatal(resp.Error)
	}
	j := resp.Job
	if j.Ceiling != DefaultCeiling || j.State.Status != JobOpen || j.State.Parts["docs"].Status != "waiting" || j.State.Parts["scan"].Status != "ready" {
		t.Fatalf("filed: %+v", j)
	}
	md, _ := os.ReadFile(filepath.Join(jobDir(j.ID), "job.md"))
	if !strings.HasPrefix(string(md), "+++\n") || !strings.Contains(string(md), "Keep COLLATE \"C\".") {
		t.Fatalf("job.md:\n%s", md)
	}

	resp = Handle(Request{Op: "job_show", ID: j.ID[:3], Lines: 5})
	if resp.Error != "" || resp.Job.Ask != spec.Ask || len(resp.Job.Parts) != 2 || resp.Job.Parts[1].After[0] != "scan" || resp.Job.DoneWhen != spec.DoneWhen {
		t.Fatalf("show: %+v %s", resp.Job, resp.Error)
	}
	if !strings.Contains(resp.Log, "· reception") || !strings.Contains(resp.Log, "Filed: Add ordered scan.") {
		t.Fatalf("log: %q", resp.Log)
	}
	appendJobLog(j.ID, "gaffer", "started scan")
	appendJobLog(j.ID, "gaffer", "scan merged\nstarting docs")
	if last, _ := JobLog(j.ID, 1); !strings.HasPrefix(last, "## ") || !strings.Contains(last, "starting docs") || strings.Contains(last, "started scan") {
		t.Fatalf("last entry: %q", last)
	}

	Handle(Request{Op: "job_add", Job: &JobSpec{Ask: "second"}})
	if resp = Handle(Request{Op: "jobs"}); len(resp.Jobs) != 2 {
		t.Fatalf("jobs: %d", len(resp.Jobs))
	}
	if resp = Handle(Request{Op: "job_show", ID: "zzzz"}); !resp.NotFound {
		t.Fatalf("missing job: %+v", resp)
	}
}
