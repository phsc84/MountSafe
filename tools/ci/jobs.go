package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/phsc84/mountsafe/tools/ci/check"
)

// floorsFile is the coverage floor of each package, relative to the root.
var floorsFile = filepath.Join("scripts", "ci", "test-coverage-floors.txt")

// static: format, vet, build, analysis (SPEC-ci 6.1).
func (r *runner) static() {
	r.step("gofmt", func() bool {
		out, err := r.output("gofmt", "-l", ".")
		if err != nil {
			return r.failStep("gofmt", err.Error())
		}
		if files := strings.TrimSpace(out); files != "" {
			fmt.Println("Not formatted with gofmt:")
			fmt.Println(files)
			r.fail("gofmt", "Not formatted with gofmt:\n"+files)
			return false
		}
		return true
	})
	// -diff changes nothing; it prints what go mod tidy would change and
	// fails then (2.1.0 stopped at its release build on stale go.sum lines).
	r.simple("tidy", false, "go", "mod", "tidy", "-diff")
	r.simple("vet", false, "go", "vet", "./...")
	r.simple("build", false, "go", "build", "-trimpath", "./...")
	r.simple("staticcheck", false, "go", "tool", "staticcheck", "./...")
	r.simple("govulncheck", false, "go", "tool", "govulncheck", "./...")
	r.step("deadcode", func() bool {
		withTest, err1 := r.output("go", "tool", "deadcode", "-test", "./...")
		without, err2 := r.output("go", "tool", "deadcode", "./...")
		unreached, testOnly := check.FilterDeadcode(withTest, without)
		ok := true
		for _, err := range []error{err1, err2} {
			if err != nil {
				ok = r.failStep("deadcode", err.Error())
			}
		}
		if len(unreached) > 0 {
			text := strings.Join(unreached, "\n")
			fmt.Println("Functions nothing calls:\n" + text)
			r.fail("deadcode: functions nothing calls", text)
			ok = false
		}
		if len(testOnly) > 0 {
			text := strings.Join(testOnly, "\n")
			fmt.Println("Production functions only tests call:\n" + text)
			r.fail("deadcode: production functions only tests call", text)
			ok = false
		}
		return ok
	})
}

// simple runs a step that fails when its command fails.
func (r *runner) simple(step string, cgo bool, name string, args ...string) bool {
	return r.step(step, func() bool {
		out, ok := r.stream(r.command(cgo, name, args...))
		if !ok {
			return r.failStep(step, out)
		}
		return true
	})
}

// test: tests, coverage floors, benchmarks, total coverage (SPEC-ci 6.2).
func (r *runner) test() {
	cover := filepath.Join(r.jobDir, "cover.out")
	var run *check.TestRun
	r.step("go test", func() bool {
		var ok bool
		run, ok = r.goTest(false, filepath.Join(r.jobDir, "test.json"), "test", "-count=1", "-json", "-coverprofile="+cover, "./...")
		return ok
	})
	r.step("floors", func() bool {
		if run == nil {
			fmt.Println("not run: go test reported nothing")
			return false
		}
		return r.floors(run)
	})
	r.simple("benchmarks", false, "go", "test", "-count=1", "-run", "^$", "-bench", ".", "-benchtime", "1x", "./...")
	r.step("total", func() bool {
		out, err := r.output("go", "tool", "cover", "-func="+cover)
		if err != nil {
			return true // shows a number only; never fails
		}
		fmt.Println(check.LastLines(out, 1))
		return true
	})
}

// goTest runs go test -json with args, saves the events to file, shows what
// go test without -v shows, and annotates every failure. It returns what
// the events reported, nil when they could not be read.
func (r *runner) goTest(cgo bool, file string, args ...string) (*check.TestRun, bool) {
	cmd := r.command(cgo, "go", args...)
	f, err := os.Create(file)
	if err != nil {
		r.setup(err)
		return nil, false
	}
	defer f.Close()
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		r.setup(err)
		return nil, false
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		r.started(err)
		return nil, false
	}
	run, readErr := check.ReadTest(io.TeeReader(pipe, f), os.Stdout)
	if readErr != nil {
		fmt.Println(readErr)
		io.Copy(io.Discard, pipe) //nolint:errcheck // drain so go test can finish
	}
	waitErr := cmd.Wait()
	r.started(waitErr)
	if run == nil {
		return nil, false
	}
	for _, failure := range run.Failures {
		r.fail(failure.Title(), check.FirstLines(failure.Output, check.MaxAnnotationLines))
	}
	return run, waitErr == nil && len(run.Failures) == 0
}

// floors compares the coverage of each package with its floor.
func (r *runner) floors(run *check.TestRun) bool {
	data, err := os.ReadFile(filepath.Join(r.root, floorsFile))
	if err != nil {
		r.setup(err)
		return false
	}
	floors, err := check.ParseFloors(bytes.NewReader(data))
	if err != nil {
		r.setup(fmt.Errorf("%s: %w", floorsFile, err))
		return false
	}
	ok := true
	for _, res := range check.CheckFloors(floors, run.Coverage, r.module) {
		fmt.Println(res)
		if res.Failed() {
			r.fail("coverage floor", res.String())
			ok = false
		}
	}
	return ok
}

// race: the tests with the race detector, which needs a gcc (SPEC-ci 6.3).
// It returns the reason when the job is skipped.
func (r *runner) race() string {
	gcc, err := exec.LookPath("gcc")
	if err != nil {
		const reason = "no gcc in PATH (see GUIDE-ci.md section 5)"
		if r.github {
			r.step("gcc", func() bool {
				fmt.Println(reason)
				r.fail("race: gcc", reason)
				return false
			})
			return ""
		}
		return reason
	}
	r.step("go test -race", func() bool {
		if out, err := exec.Command(gcc, "--version").Output(); err == nil {
			fmt.Println(check.LastLines(strings.SplitN(string(out), "\n", 2)[0], 1))
		}
		_, ok := r.goTest(true, filepath.Join(r.jobDir, "race.json"), "test", "-count=1", "-race", "-timeout", "45m", "-json", "./...")
		return ok
	})
	return ""
}

// fuzz: every fuzz target, one after the other (SPEC-ci 6.4). It returns
// the reason when the job is skipped: the module has no fuzz target yet.
func (r *runner) fuzz() string {
	var targets []check.FuzzTarget
	ok := r.step("list", func() bool {
		out, err := r.output("go", "test", "-list", "^Fuzz", "-json", "./...")
		if err != nil {
			// A package that does not build reports why in the events.
			var build bytes.Buffer
			if run, _ := check.ReadTest(strings.NewReader(out), io.Discard); run != nil {
				for _, f := range run.Failures {
					fmt.Fprintln(&build, check.FirstLines(f.Output, check.MaxAnnotationLines))
				}
			}
			fmt.Print(build.String())
			return r.failStep("list", build.String()+err.Error())
		}
		targets, err = check.FuzzTargets(strings.NewReader(out))
		if err != nil {
			fmt.Println("the listing broke:", err)
			r.fail("fuzz: list", err.Error())
			return false
		}
		for _, t := range targets {
			fmt.Printf("%s (%s)\n", t.Name, t.Package)
		}
		return true
	})
	if !ok {
		return ""
	}
	if len(targets) == 0 {
		return "no fuzz target yet"
	}
	logFile, err := os.Create(filepath.Join(r.jobDir, "fuzz.log"))
	if err != nil {
		r.setup(err)
		return ""
	}
	defer logFile.Close()
	r.step("fuzz", func() bool {
		all := true
		for _, t := range targets {
			header := fmt.Sprintf("== %s (%s)\n", t.Name, t.Package)
			fmt.Print(header)
			fmt.Fprint(logFile, header)
			cmd := r.command(false, "go", "test", "-run", "^$", "-fuzz", "^"+t.Name+"$", "-fuzztime", r.fuzztime.String(), t.Package)
			var buf bytes.Buffer
			w := io.MultiWriter(os.Stdout, logFile, &buf)
			cmd.Stdout, cmd.Stderr = w, w
			err := cmd.Run()
			r.started(err)
			if err != nil {
				all = false
				r.fail("fuzz "+t.Name, check.LastLines(buf.String(), check.MaxAnnotationLines))
				for _, p := range check.FailingInputs(buf.String()) {
					r.notes = append(r.notes, "failing input: "+p)
				}
			}
		}
		return all
	})
	return ""
}

// withoutVar returns env without the variable name (case-insensitive, as
// on Windows).
func withoutVar(env []string, name string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(strings.ToUpper(kv), strings.ToUpper(name)+"=") {
			out = append(out, kv)
		}
	}
	return out
}
