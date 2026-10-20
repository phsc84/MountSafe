package check

import (
	"fmt"
	"strings"
	"time"
)

// Jobs are the jobs of CI, in the order of ci.yml.
var Jobs = []string{"static", "test", "race", "fuzz"}

// Groups are the job groups of the command line.
var Groups = map[string][]string{
	"fast": {"static", "test"},
	"all":  {"static", "test", "race", "fuzz"},
}

// Expand turns jobs and groups into the jobs to run, in the order given; a
// group expands in place, and a job named twice runs once (CI-2).
func Expand(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("no job given")
	}
	var out []string
	seen := make(map[string]bool)
	add := func(job string) {
		if !seen[job] {
			seen[job] = true
			out = append(out, job)
		}
	}
	for _, a := range args {
		if group, ok := Groups[a]; ok {
			for _, job := range group {
				add(job)
			}
			continue
		}
		if !isJob(a) {
			return nil, fmt.Errorf("unknown job %q; jobs: %s; groups: fast, all", a, strings.Join(Jobs, ", "))
		}
		add(a)
	}
	return out, nil
}

func isJob(name string) bool {
	for _, j := range Jobs {
		if j == name {
			return true
		}
	}
	return false
}

// Status is the outcome of a job.
type Status string

const (
	Pass Status = "PASS"
	Fail Status = "FAIL"
	Skip Status = "SKIP"
)

// JobResult is what a job did.
type JobResult struct {
	Job      string
	Status   Status
	Duration time.Duration
	// Failed are the steps that failed; Reason says why a job was skipped.
	Failed []string
	Reason string
	// Notes are further lines for the summary, e.g. failing fuzz inputs.
	Notes []string
}

// Summary is the text at the end of a run (CI-6): one line per job, with
// the failed steps or the reason of a skip, then where the logs are.
func Summary(results []JobResult, out string) string {
	var b strings.Builder
	fmt.Fprintln(&b, "Summary:")
	for _, r := range results {
		fmt.Fprintf(&b, "  %-4s  %-6s  %s", r.Status, r.Job, r.Duration.Round(time.Second))
		switch {
		case r.Status == Fail && len(r.Failed) > 0:
			fmt.Fprintf(&b, "  failed: %s", strings.Join(r.Failed, ", "))
		case r.Status == Skip && r.Reason != "":
			fmt.Fprintf(&b, "  %s", r.Reason)
		}
		fmt.Fprintln(&b)
		for _, n := range r.Notes {
			fmt.Fprintf(&b, "        %s\n", n)
		}
	}
	fmt.Fprintf(&b, "Logs and artifacts: %s\n", out)
	return b.String()
}

// ExitCode is the exit code of a run whose jobs had these results (CI-5):
// 0 when every job passed or was skipped, 1 when one failed.
func ExitCode(results []JobResult) int {
	for _, r := range results {
		if r.Status == Fail {
			return 1
		}
	}
	return 0
}
