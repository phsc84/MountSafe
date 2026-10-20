package check

import (
	"regexp"
	"strings"
)

// testSupport matches deadcode lines of the test-support package, with
// either path separator; its functions exist for tests.
var testSupport = regexp.MustCompile(`^internal[\\/]testutil[\\/]`)

// FilterDeadcode returns the findings of the two deadcode passes (SPEC-ci
// 6.1 step 7): withTest is the output of deadcode -test, functions nothing
// calls; without is the output of deadcode without -test, from which it keeps
// the production functions only tests call, outside the test-support
// packages and not already among the first.
func FilterDeadcode(withTest, without string) (unreached, testOnly []string) {
	seen := make(map[string]bool)
	for _, l := range lines(withTest) {
		unreached = append(unreached, l)
		seen[l] = true
	}
	for _, l := range lines(without) {
		if !seen[l] && !testSupport.MatchString(l) {
			testOnly = append(testOnly, l)
		}
	}
	return unreached, testOnly
}

// lines splits s into its non-empty lines, without CR.
func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
