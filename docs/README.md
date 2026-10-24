# Documentation

| Document | Covers | Status |
|---|---|---|
| [CONTRIBUTING.md](CONTRIBUTING.md) | Branches, commits, pull requests, CHANGELOG, rules of the code, releases | Living |
| [GUIDE-development.md](GUIDE-development.md) | Setting up a Windows machine, the daily routine, updating Go, the spike | Living |
| [GUIDE-ci.md](GUIDE-ci.md) | The CI jobs: what each checks, how to run it locally, how to fix a failure | Living |
| [SPEC-core.md](SPEC-core.md) | Vault layout and formats, integrity (tag tables, manifests, vault state), mount and file system operations, configuration, security, test plan | Draft, not implemented |
| [SPEC-gui.md](SPEC-gui.md) | The window: vault list, unlock dialog, Create vault, Mount, Unmount, Add existing vault, Accept vault as found | Draft, not implemented |
| [SPEC-kdf.md](SPEC-kdf.md) | Argon2id parameters of the vault header: threat model, defaults, limits, memory check; identical copy in RestoreSafe | Draft, not implemented |
| [PLAN-implementation.md](PLAN-implementation.md) | The order of building MountSafe 1.0: five milestones, what each needs and when it is done, open decisions, risks | Prepared 2026-10-24 |
| [SPEC-ci.md](SPEC-ci.md) | The CI runner `tools/ci`: command line, jobs and their steps, GitHub annotations, its tests, the pre-push hook; for whoever changes it (using CI: GUIDE-ci.md) | Living |

The names follow RestoreSafe: `GUIDE-` how to do something, `SPEC-` what the program or a tool must do (living, kept up to date with the code), `PLAN-` work in progress. [CONTRIBUTING.md](CONTRIBUTING.md), the rules of the project, keeps the name GitHub recognises. A plan is deleted once its work is released; git history keeps it.

SPEC-kdf.md is shared with RestoreSafe and kept identical in both repositories: a change to one is made to the other too. SPEC-ci.md, GUIDE-ci.md and `tools/ci` are copies of RestoreSafe's that differ where MountSafe differs (no `gui` job yet).

Code comments refer to sections as "core spec 6.1", "GUI spec 5.2", "KDF spec 4.4" or "SPEC-ci 6.4".

The layer rules of the code are in the package documentation of `internal/architecture`, and its test enforces them.
