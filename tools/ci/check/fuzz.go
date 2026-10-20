package check

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// FuzzTarget is a fuzz target of a package.
type FuzzTarget struct {
	Package string
	Name    string
}

var fuzzName = regexp.MustCompile(`^Fuzz[A-Za-z0-9_]*$`)

// FuzzTargets reads the go test -list '^Fuzz' -json stream from r and
// returns the targets in the order listed (SPEC-ci 6.4).
func FuzzTargets(r io.Reader) ([]FuzzTarget, error) {
	var targets []FuzzTarget
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("not a go test -json event: %q: %w", sc.Text(), err)
		}
		if name := strings.TrimSpace(e.Output); e.Action == "output" && fuzzName.MatchString(name) {
			targets = append(targets, FuzzTarget{Package: e.Package, Name: name})
		}
	}
	return targets, sc.Err()
}

var failingInput = regexp.MustCompile(`Failing input written to (\S+)`)

// FailingInputs returns the paths of the failing inputs a fuzz run wrote,
// as its output names them.
func FailingInputs(output string) []string {
	var paths []string
	for _, m := range failingInput.FindAllStringSubmatch(output, -1) {
		paths = append(paths, m[1])
	}
	return paths
}
