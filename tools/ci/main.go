// Command ci runs the jobs of CI the same way on a developer's machine and on
// the GitHub runner (docs/SPEC-ci.md):
//
//	go run ./tools/ci fast        # static and test, before a push
//	go run ./tools/ci all         # what CI runs
//
// It imports nothing of MountSafe: it checks the code from outside, by
// running go commands. It is a copy of RestoreSafe's tools/ci without the
// gui job, which comes back with MountSafe's window.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/phsc84/mountsafe/tools/ci/check"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("ci", flag.ContinueOnError)
	out := fs.String("out", filepath.Join("sandbox", "ci"), "directory for logs and artifacts, relative to the module root")
	fuzztime := fs.Duration("fuzztime", 30*time.Second, "time per fuzz target")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: go run ./tools/ci [flags] <job or group>...\n\nJobs: %s\nGroups: fast (static test), all (static test race fuzz)\n\nFlags:\n", strings.Join(check.Jobs, ", "))
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	jobs, err := check.Expand(fs.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fs.Usage()
		return 2
	}

	r, err := newRunner(*out, *fuzztime)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var results []check.JobResult
	for _, job := range jobs {
		results = append(results, r.job(job))
	}
	fmt.Println()
	fmt.Print(check.Summary(results, r.out))
	if r.setupErr != nil {
		fmt.Fprintln(os.Stderr, r.setupErr)
		return 2
	}
	return check.ExitCode(results)
}
