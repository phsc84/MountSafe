# Contributing to MountSafe

How changes get from an idea into a release: branches, commits, pull requests, the checks CI runs, and the rules every change keeps. The same rules as RestoreSafe's, with what differs for MountSafe. How to set up a machine is in [GUIDE-development.md](GUIDE-development.md); what CI checks and how to fix a failure in [GUIDE-ci.md](GUIDE-ci.md).

## 1. Branches

| Branch | Holds | Rules |
|---|---|---|
| `main` | The latest release (none yet) | Changes only at a release, through a pull request from `dev` (section 7). GitHub shows `main` by default. |
| `dev` | The work towards the next release | Committed and pushed to directly; CI runs on every push. Keep it building and green. |
| A branch for larger work, e.g. `feature-<name>` | Work that takes longer than one release | Created from `dev` only when needed; bring it up to date with `git merge dev` regularly, and merge it back into `dev` with a pull request when it is done. Then delete it. |

GitHub enforces this for `main` with the ruleset in [.github/rulesets/release-branch.json](../.github/rulesets/release-branch.json), for everyone including the owner:

- a direct push is rejected; changes arrive through a pull request only;
- a pull request can be merged only when all four CI jobs are green;
- the branch cannot be force-pushed or deleted;
- no approval is required (GitHub does not let anyone approve their own pull request).

The JSON file is the definition. To change a rule, edit the file, commit it, and run `powershell -ExecutionPolicy Bypass -File scripts\apply-rulesets.ps1`, which replaces the ruleset on GitHub with the file's content (it needs `gh` logged in with admin rights). A change made in the GitHub settings instead is overwritten by the next run of the script. `dev` and the other branches are not protected.

Stay on `dev` for daily work. To bring your local `main` up to date without leaving it, run `git fetch origin main:main`.

## 2. Commits

- **One purpose per commit.** A commit that renames something does not also fix a bug.
- **Every commit builds and passes the tests** (`go build ./...`, `go test ./...`). A series of commits may leave an improvement unfinished, never something broken.
- **The message says what and why.** The first line is a short summary in the imperative or as a statement, at most about 72 characters. After a blank line, the body explains why the change was needed and anything a reviewer would not see in the diff. Cite spec sections as "core spec 6.4", "GUI spec 5.2", "KDF spec 4.4" or "SPEC-ci 6.4".
- **Replaced code goes in the same commit.** No commented-out code, no "old" copies, no compatibility shims inside the program.
- **Commits written with an AI assistant** end with its `Co-Authored-By:` line.

## 3. Pull requests

A pull request (PR) asks to merge one branch into another. There are two kinds: `dev` into `main` (a release, section 7), and a branch for larger work into `dev`.

1. Push the branch: `git push -u origin <branch>`.
2. Open the PR, on GitHub or with `gh pr create --base <target>`. Open it as a **draft** (`--draft`) while the work goes on.
3. The description says what the PR changes and which spec sections it implements.
4. Mark it ready (`gh pr ready`) when the work is done and CI is green.
5. Merge it with **Merge pull request** (or `gh pr merge --merge`). Only merge commits are allowed: squashing would fold the small commits of section 2 into one, and rebasing would rewrite them.

## 4. CI (GitHub Actions)

GitHub Actions runs four jobs on every push to a branch, once per commit: `static` (format, vet, build, static analysis), `test` (tests and coverage), `race` (race detector), and `fuzz` (fuzzing). A pull request into `main` can be merged only when all four are green.

Before pushing, run what CI runs, with the same runner (the tools are pinned in `go.mod`; nothing to install):

```powershell
go run ./tools/ci fast
```

`fast` is the `static` and `test` jobs; `all` adds `race` and `fuzz`. The pre-push hook runs `fast` for you (GUIDE-development.md section 3).

## 5. CHANGELOG.md

[CHANGELOG.md](../CHANGELOG.md) is written for users. A change they notice adds an entry under the unreleased version in the same commit: a new or changed feature, a changed text or behaviour, a fixed bug. Internal changes (refactoring, tests, CI, the spike) get no entry. Until the first release, there is nothing to list changes against: the changelog starts with 1.0.0.

A dependency of the program also gets an entry, under **Dependencies**: a new Go version, and a module added, updated, replaced or removed that is built into `MountSafe.exe` (go-winfsp among them). Tools that only build or check the code (the `tool` block of `go.mod`) and modules only the spike programs use get no entry.

## 6. Rules of the code

- **The specifications come first.** [SPEC-core.md](SPEC-core.md), [SPEC-gui.md](SPEC-gui.md) and [SPEC-kdf.md](SPEC-kdf.md) say what MountSafe does. Code that needs a decision the specs don't make changes the spec in the same commit or before it. SPEC-kdf.md is identical in RestoreSafe; a change to it is made in both repositories.
- **Layers.** Imports point downward only: `cmd` → `gui` → `mount` → `vault` → `config`/`logging` → `security` → `fsx`/`problem`/`buildinfo` (core spec 8). Only `internal/mount` imports go-winfsp (core spec D12). The full rules are in the package documentation of [internal/architecture](../internal/architecture/doc.go); its test fails on a violation and on a package that belongs to no layer. A new package is added to that test in the same commit.
- **Development tools stay outside.** `tools/` holds the CI runner and the spike programs. They import nothing of MountSafe, and MountSafe imports nothing of them.
- **Code copied from RestoreSafe** (core spec D3: `internal/security`, the window's widgets and Win32 helpers, `tools/ci`) says so in its package documentation, with the RestoreSafe version it was copied from. A fix found in one copy is checked against the other.
- **Imports.** The module is `github.com/phsc84/mountsafe`. Import blocks group the standard library, the module's own packages, and other modules, as `go run golang.org/x/tools/cmd/goimports -local github.com/phsc84/mountsafe -w .` sorts them.
- **The vault format is frozen once 1.0.0 is released.** Every vault written by 1.0.0 must mount with every later 1.x version. Until then, the format follows the spec, and test vaults may have to be created again.
- **No stored credentials.** MountSafe never stores a password, a recovery code or a key, and never mounts without the user (core spec 1.3).
- **Errors the user sees** name the vault and, where known, the plaintext path, and carry a remedy (core spec 9), as RestoreSafe's `problem.Error` does. The texts are sentences for the user, so staticcheck's ST1005 is turned off in [staticcheck.conf](../staticcheck.conf).
- **Plaintext never leaves the process** except through the mounted drive: no temporary files, no plaintext paths in the log below `log_level: debug` (core spec 6.4, 7.3).

## 7. Releases

Written with the first release, following RestoreSafe's: CHANGELOG and version on `dev`, a pull request from `dev` into `main`, a build from `main`, an annotated tag, a draft GitHub release with the ZIP and its checksums, tested before it is published.
