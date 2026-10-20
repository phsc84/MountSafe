# CI

What CI checks, how to run the same checks locally, and what to do when a job fails. The branch and pull request rules around it are in [CONTRIBUTING.md](CONTRIBUTING.md). Copied from RestoreSafe's GUIDE-ci.md, without the GUI smoke test.

## 1. Overview

GitHub Actions runs the checks of [.github/workflows/ci.yml](../.github/workflows/ci.yml) on a fresh Windows machine ("runner") provided by GitHub. Nothing runs on your computer; the runner starts empty every time, which also shows what depends on a developer's machine.

### When it runs

On every push to a branch of the repository, once per commit; for `main` that means after every merge. A pull request shows the result for its newest commit, the run of the push that brought it; opening a PR runs nothing more.

### The jobs

Four jobs run in parallel, each on its own runner. A pull request into `main` can be merged only when all four are green ([.github/rulesets/release-branch.json](../.github/rulesets/release-branch.json), CONTRIBUTING.md section 1).

| ID | Job (as GitHub shows it) | Checks | Runner | Section |
|---|---|---|---|---|
| `static` | static: format, vet, build, analysis | formatting, `go mod tidy`, `go vet`, the release build, staticcheck, known vulnerabilities, dead code | `go run ./tools/ci static` | 3 |
| `test` | test: tests and coverage | `go test`, the coverage floor of each package, every benchmark once | `go run ./tools/ci test` | 4 |
| `race` | race: race detector | `go test -race` | `go run ./tools/ci race` | 5 |
| `fuzz` | fuzz: fuzzing | every fuzz target for 30 seconds; skipped while there is none | `go run ./tools/ci fuzz` | 6 |

The ID is the job's key in `ci.yml` and the prefix of its name (section 7). The release build stays `CGO_ENABLED=0`; only the race detector needs cgo. Nothing in CI mounts a vault: the runner has no WinFsp, so the file system tests of core spec 14.2 run on your machine.

### Running CI locally

Each job is one run of the CI runner, `tools/ci` ([SPEC-ci.md](SPEC-ci.md)), the same on your machine and on GitHub's:

```powershell
go run ./tools/ci fast        # static and test, before a push
go run ./tools/ci all         # what CI runs
go run ./tools/ci test race   # any jobs, in this order
```

It ends with a summary (`PASS`, `FAIL` or `SKIP` per job, and the steps that failed) and writes its logs and artifacts to `sandbox\ci\<job>\` (`-out` changes that; `-fuzztime` the time per fuzz target). Exit code 0 when every job passed or was skipped, 1 when one failed; the runner prints "exit status 2" when it could not run at all (no `go.mod`, a command not found, an unreadable floors file). Without a gcc, `race` is skipped locally (section 5).

A pre-push hook runs `go run ./tools/ci fast` before every push and stops the push when a job fails. It is off until turned on once per clone; how to turn it on, skip it and turn it off is in [GUIDE-development.md](GUIDE-development.md#the-pre-push-hook). It does not replace CI: it checks the working tree, not the pushed commits, on your machine, and `--no-verify` skips it.

### Reading a result

- On the pull request: the list of checks at the bottom of the **Conversation** tab, or the **Checks** tab. A red check has **Details**, which opens the log of that job.
- All runs of the repository: the **Actions** tab. Each run belongs to one commit; each job shows every step with its full output.
- Each failure becomes an **annotation**, shown at the top of the run and on the PR: a failing test or race with its output, a package below its coverage floor, deadcode findings, unformatted files, a failing fuzz target, any other failed step with its last lines. At most ten per job; the tenth says how many more failed. The log is folded by step.
- Fuzzing and the tests upload files as **artifacts** (failing inputs, `cover.out`), listed at the bottom of the run's summary page.
- In the terminal:

```powershell
gh run list --branch dev --limit 5           # the newest runs, with their ids
gh pr checks <PR number>                     # the checks of a pull request
gh run view <run id> --log-failed            # the log of the failed steps only
gh run download <run id> -n <artifact> -D sandbox\ci-<artifact>   # an artifact
```

## 2. Fixing a failure: the routine

The same steps for every job; sections 3 to 6 add what is particular to each.

1. **Find the failing step.** Open the job's log, or `gh run view <run id> --log-failed`. Read the annotation first, if there is one.
2. **Reproduce it locally** with `go run ./tools/ci <job>`, or with the narrower command in the job's section.
3. **Fix the cause in the code**, not the check: no skipped test, no lowered floor, no disabled staticcheck check, no longer timeout just to get green. If the check itself is wrong, fix the check, and say why in the commit message.
4. **Run the job's command again**, then push. For a commit on `dev`, the fix goes in a new commit on `dev`; for a pull request, a new commit on its branch.
5. **Re-run without a change only for a runner fault** (the runner lost its network, GitHub reports an outage, a download failed): `gh run rerun <run id> --failed`. A test that fails only sometimes is a bug in the code or the test; re-running it until it is green hides it.

A job can turn red on `dev` without a change of yours: govulncheck learns of new vulnerabilities (section 3), and the fuzzer can find an input that no run found before (section 6). Fix it like any other failure; a release cannot be merged into `main` until it is green.

## 3. static: format, vet, build, analysis

`go run ./tools/ci static` runs these steps:

| Step | Fails when | Narrower, to fix it |
|---|---|---|
| gofmt | a file is not formatted | `gofmt -l .` lists the files; `gofmt -w .` formats them |
| tidy | `go.mod` or `go.sum` is not what `go mod tidy` makes of them | the step prints the difference; `go mod tidy` fixes it, then commit both files |
| vet | vet reports a finding | `go vet ./internal/<package>` |
| build | the code does not build with `CGO_ENABLED=0 -trimpath` | `go build ./...` |
| staticcheck | staticcheck reports a finding | `go tool staticcheck ./internal/<package>` |
| govulncheck | the code calls a function with a known vulnerability | `go tool govulncheck -show verbose ./...` |
| deadcode | a function has no caller, or a production function has only tests as callers (outside `internal/testutil`) | the step prints the functions |

The tools are pinned in the `tool` block of `go.mod`; nothing to install.

- **staticcheck.** Each finding names its check (e.g. `SA4006`); [staticcheck.dev/docs/checks](https://staticcheck.dev/docs/checks) explains it. Fix the code. The checks that are turned off, and why, are in [staticcheck.conf](../staticcheck.conf); turning off another one is a change of the project's rules and needs its reason there. An ignored error that is meant to be ignored gets a `//nolint:errcheck // <why>` comment.
- **govulncheck.** The report names the vulnerability (`GO-2026-…`), the module, the version that fixes it, and the call path from MountSafe's code to the vulnerable function.
  - In the standard library: update Go to the fixed version (GUIDE-development.md section 5), and an entry under **Dependencies** in CHANGELOG.md.
  - In a module: `go get <module>@<fixed version>`, `go mod tidy`, and an entry under **Dependencies** in CHANGELOG.md (CONTRIBUTING.md section 5).
  - Without a fixed version yet: avoid the vulnerable call if the code can, or wait for the fix; the job stays red until then.
- **deadcode.** Delete the function, or give it its caller: a function and its first caller go in the same commit. A helper that only tests need goes into a `_test.go` file of the package that uses it, or into `internal/testutil` when several packages need it.

## 4. test: tests and coverage

`go run ./tools/ci test` runs these steps:

| Step | Fails when | Narrower, to fix it |
|---|---|---|
| go test | a test fails, or a package does not build | one test, see below |
| floors | a package falls below its floor in [scripts/ci/test-coverage-floors.txt](../scripts/ci/test-coverage-floors.txt), or is not measured | see "Coverage floors" below |
| benchmarks | a benchmark fails | see "A failing benchmark" below |
| total | never; prints the total | |

The job uploads `cover.out` as the `coverage` artifact (locally it is in `sandbox\ci\test\`, with `test.json`, the events of `go test -json`). The console shows what `go test ./...` without `-v` shows: the package lines, the output of each failing test, and build errors. `go test` also runs every input saved under `testdata/fuzz/` (section 6).

**Test file names.** A test file is named after the file it tests (`header_test.go` for `header.go`), or after the behaviour or tool it covers when its tests span several files or none (`rollback_test.go`, `fuzz_test.go`, `bench_test.go`).

**A failing test.** The annotation shows the failing test and its lines. Run it alone, with its output:

```powershell
go test -count=1 -v -run '^TestName$' ./internal/vault
```

`-count=1` stops Go from using a cached result. A test that passes locally but fails in CI depends on something of your machine: a path, a file that exists only there, the time zone, the speed, a YubiKey, WinFsp, the NAS. The runner has none of that; the test must not need it, or must skip itself with the reason when it is missing (`t.Skip`), as RestoreSafe's YubiKey hardware tests do.

**Coverage floors.** A floor is the minimum coverage of a package, in % of statements, one line per package in `test-coverage-floors.txt` (`#` starts a comment; a line the runner cannot read stops it). Coverage counts only the package's own tests, not tests of other packages that call it.

- **A new package** gets its floor with its first tests, set a little below what they reach.
- **Below its floor:** a change added code without tests, or removed tests. Add tests for the new code. To see which lines are not covered: `go test -coverprofile=c.out ./internal/vault`, then `go tool cover -html=c.out`.
- **Not measured:** the package was renamed, moved or deleted. Change its line in `test-coverage-floors.txt` in the same commit.
- A floor is raised when a package's coverage has risen for good. It is never lowered just to let a change through; a lower floor needs its reason in the commit message.

**A failing benchmark** is a broken benchmark, not a slow one: CI runs each once and does not compare numbers. Run it alone with `go test -run '^$' -bench '^BenchmarkName$' -benchtime 1x ./internal/vault`. Benchmarks mean something only when compared on one machine, before and after a change:

```powershell
go test -count=10 -run '^$' -bench . ./internal/vault > old.txt
# make the change, then the same command into new.txt
go run golang.org/x/perf/cmd/benchstat@latest old.txt new.txt
```

## 5. race: race detector

`go run ./tools/ci race`: `go test -race ./...` with cgo and the runner's gcc, with a timeout of 45 minutes. It fails when a test fails, or when two goroutines access the same data without synchronisation and one of them writes ("data race"). The write-back cache and the commits of core spec 6.4 are where MountSafe's goroutines meet; that code needs this job most.

**Locally** it needs a MinGW-w64 gcc in `PATH` (e.g. from [winlibs.com](https://winlibs.com/)). Without one, the runner skips the job ("no gcc in PATH"); push and let CI run it. On GitHub's runner, a missing gcc fails the job. One test with the race detector:

```powershell
$env:CGO_ENABLED = "1"
go test -count=1 -race -run '^TestName$' ./internal/mount
Remove-Item Env:CGO_ENABLED
```

**Reading a race report.** The annotation shows `WARNING: DATA RACE` with two stack traces: the access that found the race, and the earlier access by another goroutine, each with the line. Below them, where each goroutine was started. The data both access is the bug.

- Fix it by synchronising the access (a `sync.Mutex`, a channel, `sync/atomic`), or by not sharing the data. Not by removing the test, `t.Parallel()`, or the goroutine.
- A race is found only when both accesses happen in a run, so a race can show once and not again. It is still a race; the report has the two lines.
- A test that fails under `-race` but not without it, without a race report, is usually too slow: the race detector makes code several times slower. A test that waits a fixed time should wait for the event instead.

## 6. fuzz: fuzzing

Fuzzing tests a parser with random inputs. Starting from the seed inputs of a target, the fuzzer changes bytes, cuts and repeats parts, thousands of times per second, and fails when the target finds an input that breaks its property: a panic, a hang, or a check of the target (e.g. "decoding what was encoded gives the same").

Every parser of bytes that can come from a vault or a file has a fuzz target (`func FuzzX(f *testing.F)`): the vault header JSON, encrypted names, file headers, chunks and tag tables, folder manifests, the vault state, the lock file, recovery codes, YubiKey data, and `config.yaml` (core spec 14.1). A new parser of such bytes gets a target in the same commit. `go run ./tools/ci fuzz` finds every target by itself (`go test -list`) and fuzzes each for 30 seconds. Until the first target exists, it skips with "no fuzz target yet" (SPEC-ci 6.4).

**Locally:**

```powershell
go run ./tools/ci -fuzztime 10s fuzz                                   # every target, 10 s each
go test -run '^$' -fuzz '^FuzzHeader$' -fuzztime 5m ./internal/vault  # one target, longer
```

**When a target fails:**

1. **Find the target and the input.** The log shows `== FuzzHeader (github.com/phsc84/mountsafe/internal/vault)` before the target, then the failure, and `Failing input written to testdata/fuzz/FuzzHeader/<name>`; the summary lists the input too.
2. **Get the input.** It is in the `fuzz-failures` artifact: `gh run download <run id> -n fuzz-failures -D sandbox\fuzz-failures`. Copy the file into the package's `testdata/fuzz/<target>/`.
3. **Reproduce it.** `go test -run '^FuzzHeader/<name>$' ./internal/vault` runs only that input and fails like CI.
4. **Fix the parser**: it returns an error for the input instead of panicking, hanging, or reading past its data. If the target's property was wrong instead (it expected something the format does not promise), fix the target.
5. **Check.** The command of step 3 passes; fuzz the target a few minutes longer locally, as one bug often hides another.
6. **Commit the fix and the input together.** The input stays in `testdata/fuzz/`, and `go test` runs it from then on as a regression test.

A failure without a saved input (the fuzzer reports that its process hung or ran out of memory) is reproduced by fuzzing that target locally until it fails; Go then saves the input.

## 7. Maintenance

- **Actions.** The workflow uses GitHub's own actions (`actions/checkout`, `actions/setup-go`, `actions/upload-artifact`) at a major version (`@v7`). When GitHub warns about an outdated version, read the action's release notes on its GitHub page and update the major version.
- **Go version.** CI takes it from `go.mod` (`go-version-file`), so the `go` line of `go.mod` is the only place to change it (GUIDE-development.md section 5).
- **Names.** Each job has a short ID: its key in `ci.yml` (`static`, `test`, `race`, `fuzz`), and the argument of `go run ./tools/ci`. The job's `name:` starts with the ID and a colon (`test: tests and coverage`). Files the runner reads in `scripts/ci/` start with the ID of their job: `test-coverage-floors.txt`.
- **The runner.** `tools/ci` (package `main`) runs the commands; `tools/ci/check` holds what can be tested without running one, tested from recorded streams in its `testdata/`, with a coverage floor of its own. It imports nothing of MountSafe, and MountSafe imports nothing of it (`internal/architecture`). It is a copy of RestoreSafe's: a fix in one that applies to the other is made in both (SPEC-ci section 1).
- **A new job** (the `gui` job, once MountSafe has a window). Give it an ID and a name as above. Add it to the runner (`check.Jobs` and a method of the runner), to `ci.yml`, to the table in section 1 and a section of its own here, and its name to the required checks in `release-branch.json`; then apply the ruleset (CONTRIBUTING.md section 1). The name in the ruleset must be the job's `name:` exactly; renaming a job means changing the ruleset in the same commit and applying it.
