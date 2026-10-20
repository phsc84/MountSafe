package check

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// update rewrites the golden files of the console output:
// go test ./tools/ci/check -run TestReadTest -update
var update = flag.Bool("update", false, "rewrite the golden files")

// readStream reads testdata/<name>.json and checks the console output
// against testdata/<name>.golden.
func readStream(t *testing.T, name string) *TestRun {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var console bytes.Buffer
	run, err := ReadTest(f, &console)
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(golden, console.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got := console.String(); got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Errorf("console output of %s:\n%s\nwant:\n%s", name, got, want)
	}
	return run
}

func TestReadTestOfPassingPackages(t *testing.T) {
	run := readStream(t, "pass")
	if len(run.Failures) != 0 {
		t.Fatalf("failures %+v", run.Failures)
	}
	if run.Coverage["example.com/fx/pass"] != 50 {
		t.Fatalf("coverage %v", run.Coverage)
	}
	if _, ok := run.Coverage["example.com/fx/notests"]; ok {
		t.Fatal("a package without test files has no coverage")
	}
}

func TestReadTestOfFailures(t *testing.T) {
	run := readStream(t, "fail")
	byTitle := make(map[string]Failure)
	for _, f := range run.Failures {
		byTitle[f.Title()] = f
	}
	for title, want := range map[string]string{
		"example.com/fx/failtest TestTwo":      "f_test.go:8: Two() = 2, want 3",
		"example.com/fx/subtest TestTable/two": "s_test.go:9: two is wrong",
		"example.com/fx/panicking TestPanics":  "panic: assignment to entry in nil map",
		"example.com/fx/mainfail":              "setup failed",
	} {
		f, ok := byTitle[title]
		if !ok || !strings.Contains(strings.Join(f.Output, "\n"), want) {
			t.Errorf("%s: want output with %q, got %+v", title, want, f)
		}
	}
	if len(run.Failures) != 4 {
		t.Errorf("a failing subtest makes its parent fail too; only the subtest is listed: %+v", run.Failures)
	}
	if f := byTitle["example.com/fx/failtest TestTwo"]; strings.Contains(strings.Join(f.Output, "\n"), "--- FAIL") {
		t.Errorf("the framing lines are not part of a failure: %q", f.Output)
	}
	if run.Coverage["example.com/fx/failtest"] != 100 {
		t.Errorf("a failing package is measured too: %v", run.Coverage)
	}
}

func TestReadTestOfABuildFailure(t *testing.T) {
	run := readStream(t, "build")
	if len(run.Failures) != 1 || !run.Failures[0].Build || run.Failures[0].Title() != "example.com/fx/buildfail [build failed]" ||
		!strings.Contains(strings.Join(run.Failures[0].Output, "\n"), "cannot use") {
		t.Fatalf("failures %+v", run.Failures)
	}
	if run.Coverage["example.com/fx/pass"] != 50 {
		t.Fatalf("the other package is measured: %v", run.Coverage)
	}
}

// race.json is written by hand in the format of go test -race -json: the
// development machine has no gcc to record one (SPEC-ci decision 1).
func TestReadTestOfADataRace(t *testing.T) {
	run := readStream(t, "race")
	if len(run.Failures) != 1 || run.Failures[0].Test != "TestCounter" || !strings.Contains(strings.Join(run.Failures[0].Output, "\n"), "WARNING: DATA RACE") {
		t.Fatalf("failures %+v", run.Failures)
	}
}

func TestReadTestRefusesWhatIsNotAnEvent(t *testing.T) {
	if _, err := ReadTest(strings.NewReader("ok  \texample.com/fx\t0.1s\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("expected an error for text that is not an event")
	}
	run, err := ReadTest(strings.NewReader("\n\n"), &bytes.Buffer{})
	if err != nil || len(run.Failures) != 0 {
		t.Fatalf("blank lines: %+v, %v", run, err)
	}
}
