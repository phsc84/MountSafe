package architecture

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
)

const module = "github.com/phsc84/mountsafe/"

// winfsp is the module of the WinFsp binding (core spec D12).
const winfsp = "github.com/winfsp/go-winfsp"

// layers lists the package groups from bottom to top. A package may import
// packages of its own layer and of lower layers, never of higher ones.
var layers = [][]string{
	{".", "internal/fsx", "internal/problem", "internal/buildinfo"}, // ".": the module root
	{"internal/security"},
	{"internal/config", "internal/logging"},
	{"internal/vault"},
	{"internal/mount"},
	{"internal/gui"},
	{"cmd"},
}

// testSupport packages exist for tests only. They are exempt from the
// layers, but production code must not import them.
var testSupport = []string{"internal/testutil", "internal/architecture"}

// devTools are development tools, such as the CI runner. They are not part
// of the program: outside the layers, and separate from it in both
// directions (TestDevToolsStandAlone).
var devTools = []string{"tools"}

// rel returns path relative to the module root: "." for the root package,
// and path itself when it is outside the module.
func rel(path string) string {
	if path == strings.TrimSuffix(module, "/") {
		return "."
	}
	return strings.TrimPrefix(path, module)
}

// inModule reports whether path is the module root or a package in it.
func inModule(path string) bool {
	return path == strings.TrimSuffix(module, "/") || strings.HasPrefix(path, module)
}

// under reports whether pkg is dir or a package below it.
func under(pkg, dir string) bool {
	return pkg == dir || strings.HasPrefix(pkg, dir+"/")
}

func underAny(pkg string, dirs []string) bool {
	for _, d := range dirs {
		if under(pkg, d) {
			return true
		}
	}
	return false
}

// layerOf returns the layer index of pkg, or -1 if it is in no layer.
func layerOf(pkg string) int {
	for i, dirs := range layers {
		if underAny(pkg, dirs) {
			return i
		}
	}
	return -1
}

// goList runs go list once for all tests.
var goList = sync.OnceValues(func() ([]byte, error) {
	return exec.Command("go", "list", "-f", `{{.ImportPath}}|{{join .Imports ","}}`, module+"...").Output()
})

// allImports returns every production (non-test) import of every package,
// keyed by the path relative to the module root; imports within the module
// are relative too.
func allImports(t *testing.T) map[string][]string {
	t.Helper()
	out, err := goList()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	pkgs := make(map[string][]string)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		path, imports, _ := strings.Cut(strings.TrimSpace(line), "|")
		pkg := rel(path)
		for _, imp := range strings.Split(imports, ",") {
			if imp != "" {
				pkgs[pkg] = append(pkgs[pkg], rel(imp))
			}
		}
	}
	return pkgs
}

// moduleImports returns the production (non-test) imports within the module
// of every package, keyed by the path relative to the module root.
func moduleImports(t *testing.T) map[string][]string {
	t.Helper()
	out, err := goList()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	pkgs := make(map[string][]string)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		path, imports, _ := strings.Cut(strings.TrimSpace(line), "|")
		pkg := rel(path)
		pkgs[pkg] = nil
		for _, imp := range strings.Split(imports, ",") {
			if inModule(imp) {
				pkgs[pkg] = append(pkgs[pkg], rel(imp))
			}
		}
	}
	if len(pkgs) == 0 {
		t.Fatal("go list found no packages")
	}
	return pkgs
}

func TestEveryPackageBelongsToALayer(t *testing.T) {
	for pkg := range moduleImports(t) {
		if layerOf(pkg) < 0 && !underAny(pkg, testSupport) && !underAny(pkg, devTools) {
			t.Errorf("package %s is in no layer; add it to layers in this test and to core spec 8", pkg)
		}
	}
}

func TestImportsPointDownward(t *testing.T) {
	for pkg, imports := range moduleImports(t) {
		if underAny(pkg, testSupport) {
			continue
		}
		from := layerOf(pkg)
		for _, imp := range imports {
			if underAny(imp, testSupport) {
				t.Errorf("%s imports the test support package %s", pkg, imp)
				continue
			}
			if to := layerOf(imp); to > from {
				t.Errorf("%s imports %s, which belongs to a higher layer", pkg, imp)
			}
		}
	}
}

// TestOnlyMountImportsWinFsp keeps the binding replaceable and the vault
// format testable without WinFsp (core spec D12). The spike programs under
// tools try the binding out and are exempt.
func TestOnlyMountImportsWinFsp(t *testing.T) {
	for pkg, imports := range allImports(t) {
		if under(pkg, "internal/mount") || underAny(pkg, devTools) {
			continue
		}
		for _, imp := range imports {
			if under(imp, winfsp) {
				t.Errorf("%s imports %s; only internal/mount uses WinFsp", pkg, imp)
			}
		}
	}
}

// TestDevToolsStandAlone keeps the development tools apart from the program:
// a package under tools imports no package of the module, and no package
// outside tools imports one under tools.
func TestDevToolsStandAlone(t *testing.T) {
	for pkg, imports := range moduleImports(t) {
		inTools := underAny(pkg, devTools)
		for _, imp := range imports {
			switch toTools := underAny(imp, devTools); {
			case inTools && !toTools:
				t.Errorf("%s imports %s; development tools import no package of the program", pkg, imp)
			case !inTools && toTools:
				t.Errorf("%s imports %s; the program imports no development tool", pkg, imp)
			}
		}
	}
}
