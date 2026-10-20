package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/phsc84/mountsafe/tools/ci/check"
)

// runner runs the jobs from the module root.
type runner struct {
	root     string // the module root
	module   string // the module path
	out      string // -out, absolute
	fuzztime time.Duration
	// github is set when running in GitHub Actions: steps are grouped and
	// failures become annotations (SPEC-ci section 7).
	github bool
	// setupErr is the first error that kept a command from running at all.
	setupErr error

	// The job being run: its name, directory, failed steps, notes and
	// annotations.
	jobName  string
	jobDir   string
	failed   []string
	notes    []string
	failures []check.Annotation
}

func newRunner(out string, fuzztime time.Duration) (*runner, error) {
	gomod, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return nil, fmt.Errorf("go env GOMOD: %w", err)
	}
	modfile := strings.TrimSpace(string(gomod))
	if modfile == "" || modfile == os.DevNull {
		return nil, errors.New("no go.mod: run the runner inside the repository")
	}
	r := &runner{root: filepath.Dir(modfile), fuzztime: fuzztime, github: os.Getenv("GITHUB_ACTIONS") == "true"}
	if !filepath.IsAbs(out) {
		out = filepath.Join(r.root, out)
	}
	r.out = out
	mod, err := r.output("go", "list", "-m")
	if err != nil {
		return nil, fmt.Errorf("go list -m: %w", err)
	}
	r.module = strings.TrimSpace(mod)
	return r, nil
}

// job runs one job; every step runs even when an earlier one failed (CI-3).
func (r *runner) job(name string) check.JobResult {
	r.jobName, r.failed, r.notes, r.failures = name, nil, nil, nil
	r.jobDir = filepath.Join(r.out, name)
	start := time.Now()
	res := check.JobResult{Job: name}
	if err := os.RemoveAll(r.jobDir); err == nil {
		err = os.MkdirAll(r.jobDir, 0o750)
		if err != nil {
			r.setup(err)
		}
	} else {
		r.setup(err)
	}

	var skip string
	switch name {
	case "static":
		r.static()
	case "test":
		r.test()
	case "race":
		skip = r.race()
	case "fuzz":
		skip = r.fuzz()
	}

	res.Duration = time.Since(start)
	res.Failed, res.Notes = r.failed, r.notes
	switch {
	case len(r.failed) > 0:
		res.Status = check.Fail
	case skip != "":
		res.Status, res.Reason = check.Skip, skip
	default:
		res.Status = check.Pass
	}
	if r.github {
		for _, line := range check.Annotations(r.failures) {
			fmt.Println(line)
		}
	} else {
		for _, f := range r.failures {
			res.Notes = append(res.Notes, f.Title)
		}
	}
	return res
}

// step runs fn as the step name of the current job, with a header, and
// records it as failed when fn returns false.
func (r *runner) step(name string, fn func() bool) bool {
	if r.github {
		fmt.Printf("::group::%s: %s\n", r.jobName, name)
	} else {
		fmt.Printf("\n== %s: %s\n", r.jobName, name)
	}
	ok := fn()
	if r.github {
		fmt.Println("::endgroup::")
	}
	if !ok {
		r.failed = append(r.failed, name)
	}
	return ok
}

// fail adds an annotation for the current job.
func (r *runner) fail(title, message string) {
	r.failures = append(r.failures, check.Annotation{Title: title, Message: message})
}

// setup records an error that kept a command from running (exit code 2).
func (r *runner) setup(err error) {
	if r.setupErr == nil {
		r.setupErr = err
	}
}

// command returns a command that runs in the module root, with CGO_ENABLED
// set as cgo says (CI-4).
func (r *runner) command(cgo bool, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Dir = r.root
	value := "0"
	if cgo {
		value = "1"
	}
	cmd.Env = append(withoutVar(os.Environ(), "CGO_ENABLED"), "CGO_ENABLED="+value)
	return cmd
}

// stream runs a command, shows its output as it comes and returns it too.
// A command that cannot start is a setup error.
func (r *runner) stream(cmd *exec.Cmd) (string, bool) {
	var buf bytes.Buffer
	w := io.MultiWriter(os.Stdout, &buf)
	cmd.Stdout, cmd.Stderr = w, w
	err := cmd.Run()
	r.started(err)
	return buf.String(), err == nil
}

// output runs a command and returns its standard output; standard error is
// shown, and is part of the error when the command fails.
func (r *runner) output(name string, args ...string) (string, error) {
	cmd := r.command(false, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	out, err := cmd.Output()
	r.started(err)
	if err != nil {
		err = fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, stderr.String())
	}
	return string(out), err
}

// started records err as a setup error when the command could not start.
func (r *runner) started(err error) {
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		r.setup(fmt.Errorf("%s: %w", r.jobName, err))
	}
}

// failStep annotates a failed step with the last lines of its output.
func (r *runner) failStep(step, output string) bool {
	r.fail(r.jobName+": "+step, check.LastLines(output, check.MaxAnnotationLines))
	return false
}
