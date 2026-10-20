# MountSafe – Development setup guide (Windows)

How to set up a Windows machine for MountSafe, the daily Git routine, and how to run the spike. The tools are the same as for RestoreSafe; a machine set up for RestoreSafe needs only WinFsp in addition (section 1).

## 1. Prerequisites

- Windows 11, 64-bit.
- **Go** in the version of the `go` line of [go.mod](../go.mod) (go.dev/dl).
- **Git** for Windows and the **GitHub CLI** (`gh`), logged in with `gh auth login`.
- **Visual Studio Code** with the Go extension (recommended).
- **WinFsp** (winfsp.dev, the MSI with its default options) for everything that mounts: the spike and, later, MountSafe itself. MountSafe loads its DLL at runtime; no cgo, no WinFsp SDK.
- Optional: a MinGW-w64 **gcc** in `PATH` for the race detector (GUIDE-ci.md section 5). Without it, CI runs it.

The CI tools (staticcheck, govulncheck, deadcode) are pinned in `go.mod` and run with `go tool`; nothing else to install.

## 2. Clone and set up

```powershell
git clone https://github.com/phsc84/MountSafe.git
cd MountSafe
git switch dev
git config core.hooksPath .githooks
go run ./tools/ci fast
```

The last command checks that the machine can build and test everything (section 3). Git identity and editor are set once per machine as for RestoreSafe (`git config --global user.name`, `user.email`, `core.autocrlf true`).

`sandbox\` is ignored by Git: put local test data, CI logs (`sandbox\ci\`) and test vaults there.

## 3. Daily workflow

```powershell
git pull                                  # on dev
# change, then
go run ./tools/ci fast                    # or let the hook run it at the push
git add <files>
git commit
git push
```

The rules for branches, commits and pull requests are in [CONTRIBUTING.md](CONTRIBUTING.md).

### The pre-push hook

A Git hook runs `go run ./tools/ci fast` (the `static` and `test` jobs of CI) before every push, and stops the push when a check fails. Commits are not checked, so push several commits at once to wait once. Turn it on once per clone:

```powershell
git config core.hooksPath .githooks
```

- It checks the working tree, not the commits being pushed: uncommitted changes are checked along with them, so commit or stash them first when they are not ready.
- `git push --no-verify` skips it once, e.g. for a push that changes only docs.
- `git config --unset core.hooksPath` turns it off again.

## 4. Running checks

```powershell
go run ./tools/ci fast        # static and test, before a push
go run ./tools/ci all         # what CI runs (race needs a gcc, section 1)
go test ./internal/vault      # one package
```

What each job checks and how to fix a failure is in [GUIDE-ci.md](GUIDE-ci.md).

## 5. Update the Go version

1. Install the new Go version.
2. `go mod edit -go=<version>` (the only place the version is set; CI reads it from `go.mod`).
3. `go get -u ./...`, `go get tool`, `go mod tidy`.
4. `go run ./tools/ci all`.
5. Commit `go.mod` and `go.sum` with "Go <version>" and an entry under **Dependencies** in CHANGELOG.md (once there is a release). If the format of `go test -json` changed, record the runner's test streams again (SPEC-ci section 8).

Keep the Go version the same as RestoreSafe's where possible: `internal/security` is shared code (core spec D3).

## 6. The spike (core spec 14.5)

Before implementation starts, the spike checks the assumptions about WinFsp, the Mac mini share and Windows that the format and the commit protocol rely on. The programs are in [tools/spike](../tools/spike/README.md); the README there says what each one checks, how to run it, and what to record. The results go into a section of core spec 14.5; any assumption that does not hold is changed in the specification before implementation starts.
