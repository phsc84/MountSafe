// Package check holds the logic of the CI runner that needs no command to
// run: reading go test -json events, coverage floors, the deadcode filter,
// fuzz target lists, GitHub annotations, job groups and the summary (SPEC-ci
// section 3.1).
package check

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// Event is one event of go test -json (and of the build output go test
// reports with it).
type Event struct {
	Action      string
	Package     string
	Test        string
	Output      string
	OutputType  string
	ImportPath  string // of build-output and build-fail events
	FailedBuild string // of a package's fail event when its build failed
}

// Failure is a failed test, a package that failed outside its tests, or a
// package whose build failed.
type Failure struct {
	Package string
	// Test is the failed test ("TestX/case" for a subtest); "" for a package.
	Test string
	// Build is set when the package did not build.
	Build bool
	// Output is what the test, the package or the build printed, without
	// the framing lines (=== RUN, --- PASS).
	Output []string
}

// Title names the failure: "<package> <test>", or the package alone.
func (f Failure) Title() string {
	if f.Test != "" {
		return f.Package + " " + f.Test
	}
	if f.Build {
		return f.Package + " [build failed]"
	}
	return f.Package
}

// TestRun is what a go test -json stream reported.
type TestRun struct {
	// Coverage is the coverage of each package, in percent, by import path.
	Coverage map[string]float64
	// Failures are in the order they were reported; a test that failed
	// only because a subtest failed is not listed apart from the subtest.
	Failures []Failure
}

var coverageLine = regexp.MustCompile(`coverage: ([0-9.]+)% of statements`)

// testKey identifies a test's output.
type testKey struct{ pkg, test string }

// ReadTest reads a go test -json stream from r and writes to w what
// go test ./... without -v prints (SPEC-ci 6.2): the package lines, the
// output of each failing test, and build errors. It returns what the stream
// reported, or an error for a line that is not an event.
func ReadTest(r io.Reader, w io.Writer) (*TestRun, error) {
	run := &TestRun{Coverage: make(map[string]float64)}
	testOut := make(map[testKey][]string)
	pkgOut := make(map[string][]string) // a package's own output, for a failure outside its tests
	open := make(map[string]bool)       // packages whose printed line has no newline yet
	endLine := func(pkg string) {
		if open[pkg] {
			fmt.Fprintln(w)
			open[pkg] = false
		}
	}
	buildOut := make(map[string][]string) // by ImportPath
	failedTests := make(map[string]bool)  // packages with a failed test

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("not a go test -json event: %q: %w", line, err)
		}
		switch {
		case e.Action == "build-output":
			fmt.Fprint(w, e.Output)
			buildOut[e.ImportPath] = append(buildOut[e.ImportPath], strings.TrimRight(e.Output, "\r\n"))
		case e.Action == "output" && e.Test != "":
			if e.OutputType != "frame" || !isRunFrame(e.Output) {
				k := testKey{e.Package, e.Test}
				testOut[k] = append(testOut[k], strings.TrimRight(e.Output, "\r\n"))
			}
		case e.Action == "output":
			if m := coverageLine.FindStringSubmatch(e.Output); m != nil {
				if v, err := strconv.ParseFloat(m[1], 64); err == nil {
					run.Coverage[e.Package] = v
				}
			}
			if printPackageLine(e.Output) || open[e.Package] {
				fmt.Fprint(w, e.Output)
				open[e.Package] = !strings.HasSuffix(e.Output, "\n")
			}
			if e.OutputType != "frame" && !strings.HasPrefix(e.Output, "coverage: ") {
				pkgOut[e.Package] = append(pkgOut[e.Package], strings.TrimRight(e.Output, "\r\n"))
			}
		case e.Action == "fail" && e.Test != "":
			k := testKey{e.Package, e.Test}
			out := testOut[k]
			delete(testOut, k)
			failedTests[e.Package] = true
			if hasFailedSubtest(run.Failures, e.Package, e.Test) {
				continue
			}
			printLines(w, out)
			run.Failures = append(run.Failures, Failure{Package: e.Package, Test: e.Test, Output: withoutFrames(out)})
		case e.Action == "pass" && e.Test != "", e.Action == "skip" && e.Test != "":
			delete(testOut, testKey{e.Package, e.Test})
		case e.Action == "fail" && e.FailedBuild != "":
			endLine(e.Package)
			run.Failures = append(run.Failures, Failure{Package: e.Package, Build: true, Output: buildOut[e.FailedBuild]})
		case e.Action == "fail":
			endLine(e.Package)
			if !failedTests[e.Package] {
				run.Failures = append(run.Failures, Failure{Package: e.Package, Output: pkgOut[e.Package]})
			}
		case e.Action == "pass", e.Action == "skip":
			endLine(e.Package)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return run, nil
}

// isRunFrame reports whether a framing line only announces a test (=== RUN,
// PAUSE, CONT, NAME); go test without -v does not print those.
func isRunFrame(s string) bool {
	return strings.HasPrefix(s, "=== ")
}

// printPackageLine reports whether go test without -v prints a package's own
// output line: everything but the PASS frame and the separate coverage line
// (the "ok" line carries the coverage).
func printPackageLine(s string) bool {
	return s != "PASS\n" && !strings.HasPrefix(s, "coverage: ")
}

// hasFailedSubtest reports whether a subtest of test in pkg is among the
// failures already: then the test failed because of it.
func hasFailedSubtest(failures []Failure, pkg, test string) bool {
	for _, f := range failures {
		if f.Package == pkg && strings.HasPrefix(f.Test, test+"/") {
			return true
		}
	}
	return false
}

func printLines(w io.Writer, lines []string) {
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}

// withoutFrames drops the "--- FAIL" and "--- PASS" lines from a test's
// output; what remains is what the test printed.
func withoutFrames(lines []string) []string {
	var out []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "--- FAIL: ") || strings.HasPrefix(t, "--- PASS: ") || strings.HasPrefix(t, "--- SKIP: ") {
			continue
		}
		out = append(out, l)
	}
	return out
}
