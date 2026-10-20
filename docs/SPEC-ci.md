# Specification: CI runner

| | |
|---|---|
| Status | Living; copied from RestoreSafe's SPEC-ci.md on 2026-10-20, without the `gui` job; decisions in section 10 |
| Readers | Whoever changes `tools/ci` or `ci.yml`. How to use CI and fix a failing job is in [GUIDE-ci.md](GUIDE-ci.md). |
| Dependencies | Go standard library only, and the tools pinned in `go.mod` |

## 1. Purpose

The checks of CI are defined once, in one Go program, `tools/ci`, which runs a job the same way on a developer's machine and on the GitHub runner:

```powershell
go run ./tools/ci fast        # static and test, before a push
go run ./tools/ci all         # what CI runs
```

[ci.yml](../.github/workflows/ci.yml) holds the triggers, the runners, checkout, Go setup and artifact upload; each job's checks are one `go run ./tools/ci <job>` step. Moving to another CI service means rewriting that thin YAML, not the checks.

`tools/ci` is a copy of RestoreSafe's. The two are not kept identical: MountSafe has no `gui` job until it has a window (section 11), and its fuzz job is skipped while the module has no fuzz target (6.4). A fix in one that applies to the other is made in both.

**Goals**

1. One command per job, identical locally and in CI.
2. No bash, awk or grep: Go is the only requirement (plus a gcc for `race`).
3. Test output read as `go test -json` events instead of text patterns.

## 2. Constraints

1. **Job IDs and names are fixed.** The keys `static`, `test`, `race`, `fuzz` and their `name:` values in `ci.yml` are the required checks of [release-branch.json](../.github/rulesets/release-branch.json); renaming one means changing the ruleset in the same commit and applying it (GUIDE-ci.md section 7).
2. **Standard library only.** `tools/ci` imports nothing outside the standard library and no package of the module. External tools are called through `go tool` (staticcheck, govulncheck, deadcode, as pinned in `go.mod`), `go`, `gofmt` and `gcc` (for the check in 6.3 only).
3. **The project's rules apply to the runner too.** It is vetted, staticchecked and dead-code checked like the rest; its logic package has a coverage floor (section 8); it is in `internal/architecture` (section 3.2).
4. **No check becomes weaker.** A change of the runner keeps every check of section 4, or the change says why in its commit and in this spec.

## 3. Layout

### 3.1 Packages

| Path | Package | Holds |
|---|---|---|
| `tools/ci/` | `main` | Flags, job dispatch, running commands, console output. Thin: no parsing. |
| `tools/ci/check/` | `check` | Everything that can be tested without running a command: reading `go test -json` events, coverage floors, the deadcode filter, fuzz target lists, the summary, GitHub annotations and their escaping. |
| `tools/ci/check/testdata/` | | Recorded `go test -json` streams and the console output expected from them (section 8). |

`tools/` and not `cmd/`: `cmd/` holds the programs that ship.

### 3.2 Architecture test

`internal/architecture` puts every package of the module in a layer (`security` ... `cmd`) or in `testSupport`; packages under `tools` are a third group, `devTools`: the CI runner and the spike programs (GUIDE-development.md section 6). They are not part of MountSafe: they check or try things from outside and need none of MountSafe's packages.

`TestDevToolsStandAlone` enforces the separation both ways: a package under `tools` imports no package of the module, and no package outside `tools` imports one under `tools`. `TestImportsPointDownward` needs nothing for them.

## 4. The checks of each job

| Job | Check | Section |
|---|---|---|
| static | `gofmt -l .` is empty | 6.1 step 1 |
| static | `go mod tidy -diff` shows no difference | 6.1 step 2 |
| static | `go vet ./...` | 6.1 step 3 |
| static | `go build -trimpath ./...` with `CGO_ENABLED=0` | 6.1 step 4 |
| static | `go tool staticcheck ./...` | 6.1 step 5 |
| static | `go tool govulncheck ./...` | 6.1 step 6 |
| static | deadcode, with and without `-test` | 6.1 step 7 |
| test | `go test -count=1 -json -coverprofile ./...`, a failure annotation per failing test | 6.2 step 1 |
| test | coverage floors | 6.2 step 2 |
| test | every benchmark once | 6.2 step 3 |
| test | total coverage | 6.2 step 4 |
| race | `go test -race -timeout 45m ./...` with `CGO_ENABLED=1`, annotations as in test | 6.3 |
| fuzz | every fuzz target for 30 s | 6.4 |

## 5. Command line

```
go run ./tools/ci [flags] <job or group>...
```

| Argument | Runs |
|---|---|
| `static`, `test`, `race`, `fuzz` | that job |
| `fast` | `static test` |
| `all` | `static test race fuzz` |

| Flag | Default | Meaning |
|---|---|---|
| `-out <dir>` | `sandbox/ci` | Directory for logs and artifacts; each job writes to `<out>/<job>/`, emptied at the job's start. Relative to the module root. |
| `-fuzztime <duration>` | `30s` | Time per fuzz target. |
| `-h` | | Usage, with the jobs and groups. |

| Requirement | |
|---|---|
| CI-1 | The runner finds the module root (`go env GOMOD`) and runs every command there, so it works from any directory of the repository. |
| CI-2 | Jobs run one after the other, in the order given (a group expands in place; a job named twice runs once). Every job runs even when an earlier one failed. |
| CI-3 | Within a job, every step runs even when an earlier step failed, except where a step needs an earlier one's output and that output is missing (then the step is reported as not run). The job fails when any step failed. |
| CI-4 | The runner sets `CGO_ENABLED` for each command it starts (`0` everywhere, `1` for the race tests) and does not depend on the caller's environment for it. |
| CI-5 | Exit code 0: every job passed or was skipped. 1: a check failed. 2: the runner could not run (unknown job, no `go.mod`, a command not found, an unreadable floors file). |
| CI-6 | At the end, a summary: one line per job with `PASS`, `FAIL` or `SKIP`, its duration, and for `FAIL` the failed steps, for `SKIP` the reason; then the path of `-out`. |
| CI-8 | No step changes a tracked file. Files written outside `-out` are only those `go test -fuzz` writes into `testdata/fuzz/` for a failing input. |

(CI-7, "`gui` is never part of a group", comes back with the `gui` job.)

## 6. Jobs

Each step prints a header with its name before it starts. A command's output is shown as it comes (streamed), except where 6.2 says otherwise.

### 6.1 static

| Step | Command | Fails when |
|---|---|---|
| 1. gofmt | `gofmt -l .` | it lists a file; the files are printed |
| 2. tidy | `go mod tidy -diff` (changes nothing) | exit code ≠ 0: `go.mod` or `go.sum` differs from what `go mod tidy` makes; the difference is printed |
| 3. vet | `go vet ./...` | exit code ≠ 0 |
| 4. build | `go build -trimpath ./...`, `CGO_ENABLED=0` | exit code ≠ 0 |
| 5. staticcheck | `go tool staticcheck ./...` | exit code ≠ 0 |
| 6. govulncheck | `go tool govulncheck ./...` | exit code ≠ 0 |
| 7. deadcode | `go tool deadcode -test ./...`, then `go tool deadcode ./...` | see below |

Step 7:

- the first pass fails on any line it prints ("functions nothing calls");
- the second pass fails on any line that is not under `internal/testutil/` (either path separator) and not already printed by the first pass ("production functions only tests call");
- a non-zero exit code of either command fails the step.

### 6.2 test

| Step | Does | Fails when |
|---|---|---|
| 1. go test | `go test -count=1 -json -coverprofile=<out>/test/cover.out ./...`, `CGO_ENABLED=0`; the event stream is saved to `<out>/test/test.json` | a package or test fails, or a package does not build |
| 2. floors | compares each package's coverage with `scripts/ci/test-coverage-floors.txt` | see below |
| 3. benchmarks | `go test -count=1 -run '^$' -bench . -benchtime 1x ./...`, `CGO_ENABLED=0` | exit code ≠ 0 |
| 4. total | `go tool cover -func=<out>/test/cover.out`, last line printed | never |

**Console output of step 1.** What `go test ./...` without `-v` prints: the package lines (`ok`, `FAIL`, `?`) as they come, the full output of each failing test, and build errors (`build-output` events). Passing tests print nothing.

**Reading the events.** A failure is an event with `"Action":"fail"`: with `Test` set for a test (subtests included, e.g. `TestX/case`), without it for a package (a panic outside a test, `TestMain`, a timeout), and with `FailedBuild` for a build failure. A test's output is the `output` events with the same `Package` and `Test`. A package's coverage is the `output` event of that package matching `coverage: <n>% of statements` (it is printed for failing packages too).

**Floors file.** One package per line: path relative to the module (`internal/vault`), whitespace, a number. Lines starting with `#` and blank lines are ignored; CRLF and LF are both accepted. Any other line is an error (exit 2).

**Floors check.** For each package in the file: below its floor fails ("<pkg>: 84.9 % is below its floor of 85 %"); not in the events fails ("<pkg>: not measured (floor 85 %)"); otherwise one line with coverage and floor. Packages are listed sorted. A package at exactly its floor passes. Packages without a floor are not checked.

### 6.3 race

`go test -count=1 -race -timeout 45m -json ./...`, `CGO_ENABLED=1`, events saved to `<out>/race/race.json`, console output and failures as in 6.2 step 1. A data race shows as a failing test whose output contains `WARNING: DATA RACE`; the report is that test's output, no extra pattern needed.

Before it starts, the runner looks for `gcc` in `PATH` and prints its version line. Without a gcc: in CI (`GITHUB_ACTIONS=true`) the job fails; locally it is skipped with the reason "no gcc in PATH (see GUIDE-ci.md section 5)" (decision 1).

### 6.4 fuzz

1. **List.** `go test -list '^Fuzz' -json ./...`; a target is an `output` event of a package whose text (trimmed) starts with `Fuzz` and is a Go identifier. A listing that fails or cannot be read fails the job.
2. **No target found:** the job is skipped with the reason "no fuzz target yet" (decision 3). RestoreSafe fails here instead, because it has targets and an empty list means the listing broke; MountSafe takes that rule over with its first fuzz target.
3. **Fuzz** each target, one after the other: `go test -run '^$' -fuzz '^<name>$' -fuzztime <fuzztime> <package>`, `CGO_ENABLED=0`, output streamed and copied to `<out>/fuzz/fuzz.log`, headed `== <name> (<package>)`. Plain output, not `-json`: fuzzing reports a crash in text.
4. A failing target fails the job; the others still run. When the output contains `Failing input written to <path>`, the summary lists that path.

## 7. GitHub integration

The runner knows it runs in GitHub Actions by `GITHUB_ACTIONS=true`. Only then:

| Requirement | |
|---|---|
| CI-20 | Each step's output is wrapped in `::group::<job>: <step>` and `::endgroup::`, so the log is folded by step. |
| CI-21 | Each failure becomes an `::error title=<title>::<message>` annotation: per failing test (title `<package> <test>`, message its output, at most 40 lines), per package below or missing its floor, per deadcode pass with findings, per unformatted-file list, per failing fuzz target, and one per other failed step (title the step, message the last 40 lines of its output). |
| CI-22 | At most 10 error annotations per job run (GitHub shows no more per step); when there are more, the 10th says "and <n> more failures, see the log". |
| CI-23 | In annotation messages, `%`, CR and LF are escaped as `%25`, `%0D`, `%0A`; in titles also `:` and `,` (`%3A`, `%2C`). |

Locally the same failures are printed as plain text in the summary (CI-6).

### 7.1 ci.yml

Every job has this shape; only `name`, `timeout-minutes` and the artifact differ:

```yaml
  test:
    name: "test: tests and coverage"
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod
      - run: go run ./tools/ci -out "${{ runner.temp }}/ci" test
      - uses: actions/upload-artifact@v7
        if: always()
        with:
          name: coverage
          path: ${{ runner.temp }}/ci/test/cover.out
```

- **Trigger:** `push` to any branch (`branches: ["**"]`, so no tag), and no `pull_request`. Each commit runs once; a pull request shows the checks of its newest commit, and the ruleset of `main` requires them by name on that commit, whichever event ran them. Skipping `pull_request` runs with a job-level `if` is not an option: a skipped job reports Success under the same name as the push run, which could hide a failure from the ruleset.
- The steps run in the runner's default shell, PowerShell 7; no `defaults.run.shell`.
- Artifacts: `coverage` from `ci/test/cover.out` (always); `fuzz-failures` from `internal/**/testdata/fuzz/` (on failure).

## 8. Tests of the runner

`tools/ci/check` is tested from recorded inputs in `testdata/`, recorded with the Go version of `go.mod` from a small fixture module (a package per case); the recordings were taken over from RestoreSafe. The data race stream is written by hand in the same format, because recording one needs a gcc. After a Go update, record the streams again when the format of `go test -json` changed.

| Area | Cases |
|---|---|
| Events | all pass; a failing test; a failing subtest; a panic in a test; a package failing outside a test; a build failure; a data race; coverage of a passing and of a failing package; a package without test files |
| Console output | for the cases above, the printed text matches a golden file (`go test ./tools/ci/check -run TestReadTest -update` rewrites them) |
| Floors file | comments, blank lines, CRLF, a malformed line (error), a duplicate package (error); the repository's own floors file parses |
| Floors check | above, exactly at, below the floor; not measured; output sorted |
| Deadcode filter | both passes empty; findings in pass 1 only; test-only functions inside and outside the test-support package, with `/` and `\` |
| Fuzz list | several packages, a package without targets, a line that is not an identifier |
| Annotations | escaping of message and title; the limit of 10 |
| Groups | `fast`, `all`, a job named twice, an unknown job (also `gui`) |

- `tools/ci/check` has a coverage floor of 90 in `test-coverage-floors.txt`.
- `tools/ci` (`main`) has no floor: it runs commands. A change of it is checked by running the jobs it changes, locally and in CI.
- No fuzz target: the runner reads output of `go`, not bytes from a vault.

## 9. Docs

[GUIDE-ci.md](GUIDE-ci.md) is for the user of CI: what each job checks, how to read a result, how to reproduce and fix a failure. This spec is for whoever changes the runner. A change of what the user sees (a job, a step, a command, the summary, an annotation) changes both. [CONTRIBUTING.md](CONTRIBUTING.md) section 4 and [GUIDE-development.md](GUIDE-development.md) section 3 name `go run ./tools/ci fast` before a push and the pre-push hook (section 10.1).

## 10. Decisions

Taken over from RestoreSafe (decided by the owner, 2026-10-14):

1. **Race without a gcc, locally: skipped.** The summary shows `SKIP` with the reason, exit code 0. The race detector runs on the GitHub runner on every push and is a required check for `main`. On the runner, a missing gcc fails the job (6.3).
2. **Pre-push hook: yes.** Commits stay unchecked; pushes are checked, so pushing less often keeps the wait small.

New for MountSafe (2026-10-20):

3. **Fuzz without targets: skipped**, so CI is green from the first commit. The job stays a required check; it fuzzes from the first target on (6.4).

### 10.1 The pre-push hook

| Requirement | |
|---|---|
| CI-30 | `.githooks/pre-push` runs `go run ./tools/ci fast` from the repository root and passes its exit code on; a non-zero code stops the push. It is a two-line `sh` script (`#!/bin/sh`, `exec go run ./tools/ci fast`), which Git for Windows runs with its own `sh`. |
| CI-31 | `.gitattributes` has `.githooks/* text eol=lf`, so the hook keeps LF line endings on Windows. |
| CI-32 | The hook is turned on per clone with `git config core.hooksPath .githooks` (Git does not take hooks from a clone by itself). GUIDE-development.md says so, and that `git push --no-verify` skips it once, e.g. for a docs-only push. |
| CI-33 | GUIDE-development.md notes that the hook checks the working tree, not the pushed commits: uncommitted changes are checked along with them. |

## 11. Not part of this spec

- The `gui` job (RestoreSafe's SPEC-ci 6.5): comes back with MountSafe's window, with a smoke test of its own.
- A job that mounts a vault: the GitHub runner has no WinFsp; the file system tests of core spec 14.2 run locally until it does.
- Moving to another forge or CI service, mirroring the repository, self-hosted runners.
- Changing schedules or which checks are required. The trigger is in 7.1.
- Running jobs in parallel locally.
- Releases, `scripts/apply-rulesets.ps1`.
- A job summary page (`GITHUB_STEP_SUMMARY`); annotations and the log are enough for now.
