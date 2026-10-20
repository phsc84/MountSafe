package check

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Floors are the coverage floors, in percent, by package path relative to
// the module ("internal/fsx").
type Floors map[string]float64

// ParseFloors reads a floors file: one package per line, its path and a
// number separated by whitespace. Lines starting with # and blank lines are
// ignored; CRLF and LF are accepted. Any other line, and a package listed
// twice, is an error (SPEC-ci 6.2).
func ParseFloors(r io.Reader) (Floors, error) {
	floors := make(Floors)
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("floors line %d: want a package and a number, got %q", n, line)
		}
		v, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || v < 0 || v > 100 {
			return nil, fmt.Errorf("floors line %d: %q is not a percentage", n, fields[1])
		}
		if _, dup := floors[fields[0]]; dup {
			return nil, fmt.Errorf("floors line %d: %s is listed twice", n, fields[0])
		}
		floors[fields[0]] = v
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return floors, nil
}

// FloorResult is the result of the floors check for one package.
type FloorResult struct {
	Package  string
	Floor    float64
	Coverage float64
	Measured bool
}

// Failed reports whether the package is below its floor or was not
// measured. A package at exactly its floor passes.
func (r FloorResult) Failed() bool { return !r.Measured || r.Coverage < r.Floor }

// String is the line of the package in the floors check.
func (r FloorResult) String() string {
	switch {
	case !r.Measured:
		return fmt.Sprintf("%s: not measured (floor %g %%)", r.Package, r.Floor)
	case r.Coverage < r.Floor:
		return fmt.Sprintf("%s: %.1f %% is below its floor of %g %%", r.Package, r.Coverage, r.Floor)
	}
	return fmt.Sprintf("%-36s %5.1f %%  (floor %g %%)", r.Package, r.Coverage, r.Floor)
}

// CheckFloors compares the coverage of each package with a floor, sorted by
// package. coverage is keyed by import path; module is the module path,
// which the floors leave out. Packages without a floor are not checked.
func CheckFloors(floors Floors, coverage map[string]float64, module string) []FloorResult {
	results := make([]FloorResult, 0, len(floors))
	for pkg, floor := range floors {
		cov, ok := coverage[module+"/"+pkg]
		results = append(results, FloorResult{Package: pkg, Floor: floor, Coverage: cov, Measured: ok})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Package < results[j].Package })
	return results
}
