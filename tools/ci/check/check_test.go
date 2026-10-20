package check

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseFloors(t *testing.T) {
	t.Parallel()

	floors, err := ParseFloors(strings.NewReader("# comment\r\n\r\ninternal/fsx   92\r\ninternal/logging 91.5\n  \n"))
	if err != nil || !reflect.DeepEqual(floors, Floors{"internal/fsx": 92, "internal/logging": 91.5}) {
		t.Fatalf("floors %v, %v", floors, err)
	}
	for name, text := range map[string]string{
		"one field":   "internal/fsx\n",
		"no number":   "internal/fsx ninety\n",
		"over 100":    "internal/fsx 101\n",
		"three words": "internal/fsx 90 extra\n",
		"duplicate":   "internal/fsx 90\ninternal/fsx 91\n",
	} {
		if _, err := ParseFloors(strings.NewReader(text)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestTheFloorsFileOfTheRepositoryParses(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "ci", "test-coverage-floors.txt"))
	if err != nil {
		t.Fatal(err)
	}
	floors, err := ParseFloors(strings.NewReader(string(data)))
	if err != nil || floors["tools/ci/check"] != 90 {
		t.Fatalf("floors of the repository: %v, %v", floors, err)
	}
}

func TestCheckFloors(t *testing.T) {
	t.Parallel()

	floors := Floors{"b/above": 80, "a/exact": 85, "c/below": 90, "d/missing": 70}
	coverage := map[string]float64{"m/b/above": 81.2, "m/a/exact": 85, "m/c/below": 89.9, "m/e/nofloor": 1}
	results := CheckFloors(floors, coverage, "m")
	var got []string
	var failed []string
	for _, r := range results {
		got = append(got, r.Package)
		if r.Failed() {
			failed = append(failed, r.String())
		}
	}
	if !reflect.DeepEqual(got, []string{"a/exact", "b/above", "c/below", "d/missing"}) {
		t.Fatalf("sorted packages, without those that have no floor: %v", got)
	}
	want := []string{"c/below: 89.9 % is below its floor of 90 %", "d/missing: not measured (floor 70 %)"}
	if !reflect.DeepEqual(failed, want) {
		t.Fatalf("failures %q, want %q", failed, want)
	}
	if s := results[0].String(); !strings.Contains(s, "85.0 %  (floor 85 %)") {
		t.Fatalf("a package at its floor passes: %q", s)
	}
}

func TestFilterDeadcode(t *testing.T) {
	t.Parallel()

	if u, o := FilterDeadcode("", "\r\n"); u != nil || o != nil {
		t.Fatalf("both empty: %v, %v", u, o)
	}
	unused := `internal\vault\x.go:3:6: unreachable func: Unused`
	withTest := unused + "\r\n"
	without := strings.Join([]string{
		unused,
		`internal\vault\x.go:9:6: unreachable func: onlyTests`,
		`internal/mount/y.go:2:6: unreachable func: alsoOnlyTests`,
		`internal\testutil\fixture.go:42:6: unreachable func: NewFixture`,
		`internal/testutil/scenario/scenario.go:90:6: unreachable func: Build`,
		`internal\testutilx\other.go:1:6: unreachable func: notTestSupport`,
	}, "\n")
	unreached, testOnly := FilterDeadcode(withTest, without)
	if !reflect.DeepEqual(unreached, []string{unused}) {
		t.Fatalf("pass 1: %q", unreached)
	}
	want := []string{
		`internal\vault\x.go:9:6: unreachable func: onlyTests`,
		`internal/mount/y.go:2:6: unreachable func: alsoOnlyTests`,
		`internal\testutilx\other.go:1:6: unreachable func: notTestSupport`,
	}
	if !reflect.DeepEqual(testOnly, want) {
		t.Fatalf("pass 2: %q, want %q", testOnly, want)
	}
}

func TestFuzzTargets(t *testing.T) {
	t.Parallel()

	f, err := os.Open(filepath.Join("testdata", "list.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	targets, err := FuzzTargets(f)
	want := []FuzzTarget{{"example.com/fx/fuzzy", "FuzzOne"}, {"example.com/fx/fuzzy", "FuzzTwo"}}
	if err != nil || !reflect.DeepEqual(targets, want) {
		t.Fatalf("targets %v, %v; want %v (the ok lines and the package without targets are no targets)", targets, err, want)
	}
	if _, err := FuzzTargets(strings.NewReader("FuzzOne\n")); err == nil {
		t.Fatal("expected an error for text that is not an event")
	}
}

func TestFailingInputs(t *testing.T) {
	t.Parallel()

	out := "--- FAIL: FuzzParse (0.21s)\n    Failing input written to testdata\\fuzz\\FuzzParse\\0f6c\n    To re-run:\n"
	if got := FailingInputs(out); !reflect.DeepEqual(got, []string{`testdata\fuzz\FuzzParse\0f6c`}) {
		t.Fatalf("FailingInputs = %q", got)
	}
	if FailingInputs("ok") != nil {
		t.Fatal("no failing input")
	}
}

func TestAnnotationsEscapeAndStopAtTheLimit(t *testing.T) {
	t.Parallel()

	got := Annotations([]Annotation{{Title: "pkg: TestA, case", Message: "100% wrong\r\nline 2"}})
	if want := "::error title=pkg%3A TestA%2C case::100%25 wrong%0D%0Aline 2"; len(got) != 1 || got[0] != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	var many []Annotation
	for range 13 {
		many = append(many, Annotation{Title: "t", Message: "m"})
	}
	got = Annotations(many)
	if len(got) != MaxAnnotations || !strings.Contains(got[MaxAnnotations-1], "and 4 more failures, see the log") {
		t.Fatalf("13 failures: %d annotations, last %q", len(got), got[len(got)-1])
	}
	if len(many) != 13 {
		t.Fatal("Annotations must not change its argument")
	}
	if got := Annotations(many[:MaxAnnotations]); len(got) != MaxAnnotations || strings.Contains(got[MaxAnnotations-1], "more failures") {
		t.Fatalf("exactly the limit: %q", got[len(got)-1])
	}
}

func TestFirstAndLastLines(t *testing.T) {
	t.Parallel()

	if got := FirstLines([]string{"a", "b", "c"}, 2); got != "a\nb" {
		t.Fatalf("FirstLines = %q", got)
	}
	if got := LastLines("a\r\nb\r\nc\r\n", 2); got != "b\nc" {
		t.Fatalf("LastLines = %q", got)
	}
	if got := LastLines("a", 5); got != "a" {
		t.Fatalf("LastLines of one line = %q", got)
	}
}

func TestExpand(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"fast"}, []string{"static", "test"}},
		{[]string{"all"}, []string{"static", "test", "race", "fuzz"}},
		{[]string{"test", "fast", "race", "test"}, []string{"test", "static", "race"}},
	} {
		got, err := Expand(tc.args)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Expand(%v) = %v, %v; want %v", tc.args, got, err, tc.want)
		}
	}
	for _, args := range [][]string{nil, {"lint"}, {"gui"}, {"fast", "Test"}} {
		if _, err := Expand(args); err == nil {
			t.Errorf("Expand(%v): expected an error", args)
		}
	}
}

func TestSummaryAndExitCode(t *testing.T) {
	t.Parallel()

	results := []JobResult{
		{Job: "static", Status: Pass, Duration: 61 * time.Second},
		{Job: "test", Status: Fail, Duration: 90 * time.Second, Failed: []string{"go test", "floors"}, Notes: []string{"example.com/x TestY"}},
		{Job: "race", Status: Skip, Reason: "no gcc in PATH"},
	}
	want := "Summary:\n" +
		"  PASS  static  1m1s\n" +
		"  FAIL  test    1m30s  failed: go test, floors\n" +
		"        example.com/x TestY\n" +
		"  SKIP  race    0s  no gcc in PATH\n" +
		"Logs and artifacts: out\n"
	if got := Summary(results, "out"); got != want {
		t.Fatalf("summary:\n%s\nwant:\n%s", got, want)
	}
	if ExitCode(results) != 1 || ExitCode(results[:1]) != 0 || ExitCode(results[2:]) != 0 {
		t.Fatal("a failed job makes the exit code 1; pass and skip make it 0")
	}
}

func TestFailureTitles(t *testing.T) {
	t.Parallel()

	for f, want := range map[*Failure]string{
		{Package: "p", Test: "TestX/case"}: "p TestX/case",
		{Package: "p", Build: true}:        "p [build failed]",
		{Package: "p"}:                     "p",
	} {
		if got := f.Title(); got != want {
			t.Errorf("Title() = %q, want %q", got, want)
		}
	}
}
