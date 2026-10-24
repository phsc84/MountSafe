# Plan: Implementation of MountSafe 1.0

| | |
|---|---|
| Status | Prepared 2026-10-24, decisions O1–O4 taken; not started |
| Implements | [SPEC-core.md](SPEC-core.md) Draft 0.7, [SPEC-gui.md](SPEC-gui.md) Draft 0.5, [SPEC-kdf.md](SPEC-kdf.md) Draft 0.3 |
| Prepared by | The spike of core spec 14.5 (done 2026-10-24, results in its table) and the review of core spec 4–7 (2026-10-24) |
| Branch | `dev`; a milestone that takes long may get `feature-<name>` (CONTRIBUTING.md section 1) |
| Copied code | From RestoreSafe `v2.1.1` (`45e02c5`) unless an item says otherwise |

## 1. Purpose

The order in which MountSafe 1.0 is built, in five milestones. Each milestone ends with everything green in CI and something that can be tried; none needs the next one to be tested. The specifications say what MountSafe does; this plan says only in which order, what each step needs, and when it is done. When an item needs a decision the specs don't make, the spec changes first (CONTRIBUTING.md section 6).

The plan is deleted once 1.0.0 is released; git history keeps it (docs/README.md).

## 2. Rules for every item

1. **Spec first.** An item implements spec sections; code comments cite them ("core spec 5.3"). A gap found while coding is closed in the spec in the same commit or before.
2. **Green at every commit** (CONTRIBUTING.md section 2), with `go run ./tools/ci fast` before each push (the hook does it).
3. **Tests with the code.** A package gets its tests and its coverage floor in `scripts/ci/test-coverage-floors.txt` with its first commit, a little below what the tests reach. Every parser of bytes from a vault or a file gets a fuzz target in the same commit (GUIDE-ci.md section 6); the first one turns the fuzz job from SKIP to running (SPEC-ci 6.4), and from then on an empty list fails it again as in RestoreSafe.
4. **Known-answer vectors.** Each format of core spec 5 gets fixed test vectors in `testdata/` (bytes written with fixed keys, nonces and inputs, and what they decode to). From 1.0.0 they describe the released format and never change, like RestoreSafe's `internal/format/testdata/v2.0.0/`.
5. **Copied code** names its source in the package documentation ("Copied from RestoreSafe v2.1.1, internal/security/cryptox; changed: …") (CONTRIBUTING.md section 6).
6. **New packages** are in the layer table of core spec 8 and in `internal/architecture` before their first code.

## 3. Milestones

### M1 – Foundations and formats

The bottom layers and every on-disk format, as pure functions over bytes. No vault on disk yet.

| # | Item | Spec | Done when |
|---|---|---|---|
| M1.1 | `internal/problem` and `internal/buildinfo`, copied | core 8, 9 | tests copied and passing |
| M1.2 | `internal/fsx`: long-path helper, `SetFileTime`, `MoveFileEx` without replace, `GetDriveType` and the error mode, `GlobalMemoryStatusEx`, process lookup (image name, start time) | core 6.2, 7.5, 12; KDF 4.4 | unit tests, including a path over 260 characters in a temporary folder |
| M1.3 | `internal/security`: `cryptox`, `recovery`, `yubikey` copied; Argon2id defaults 4 GiB / 2 / 4 and the limits of KDF spec 4.2, read limits equal to them (KDF 7); the memory check of KDF 4.4 on top of `fsx`; WebAuthn RP ID `mountsafe` | core 8; KDF 4, 7 | tests copied and adapted; the YubiKey hardware tests skip without a YubiKey as in RestoreSafe |
| M1.4 | Key hierarchy (HKDF of 5.1) and the key set with its slots, adapted from RestoreSafe's `format/container/keyset.go` | core 5.1 | slot tests as in 14.1; a RestoreSafe slot does not open as a MountSafe slot (AAD prefix) |
| M1.5 | Vault header: encode, decode, HMAC, the two copies and their comparison before unlocking | core 5.1 | 14.1 header tests; fuzz target for the header JSON |
| M1.6 | Encrypted names, short and long form | core 5.2, 4 | 14.1 name tests; fuzz target |
| M1.7 | File entries: header, chunks, tag table, file digest, size formula | core 5.3 | 14.1 file entry and tag table tests; fuzz targets for chunks and tag table |
| M1.8 | Folder manifest and vault state | core 5.7, 5.8 | 14.1 manifest and state tests; fuzz targets |
| M1.9 | Known-answer vectors for M1.4–M1.8 | rule 4 | vectors in `internal/vault/testdata/` |

**Result:** every format MountSafe writes exists, round-trips and rejects every tampering of 14.1 that can be tested on bytes.

### M2 – Vault operations, without WinFsp

The vault as a library: everything core spec 6 and 7 do, on a folder, called from Go. This milestone carries most of the risk, so it comes before any mounting.

| # | Item | Spec | Done when |
|---|---|---|---|
| M2.1 | **The file system interface** of `internal/vault`: exactly the operations the vault needs (open with share modes, read and write at offsets, flush, truncate, rename without replace, delete, list, make and remove folders, set times). Two implementations: the real one on Windows files through `fsx`, and one in memory for tests that can **fail after the n-th write**, return **network errors**, and **lose or tear writes since the last flush** (the power loss of 6.4) | core 8; 14.3 | both pass the same interface tests |
| M2.2 | Create a vault | core 6.1 | a created vault opens; its files are as 4 and 5 say |
| M2.3 | The lock: open, holder line, orphan detection | core 6.2 steps 2–3 | 14.1 lock tests, including a reused process ID |
| M2.4 | Unlock and open: header copies, state, recovery of an intent, the older-state check, `seen_state` | core 6.2 steps 4–7, 6.4 | tests for every branch |
| M2.5 | Directory index with the checks of folder load | core 6.2, 6.8 | 14.1 directory index tests; every row of 6.8 that concerns folders |
| M2.6 | Reading and writing files: chunks covering a range in one request, tag table checks, read-modify-write, extend, truncate, overwrite | core 5.3, 6.3 | 14.1 file entry tests through the vault API |
| M2.7 | Commits: snapshot, intent – data – commit, ordering rules, coalesced writes, the 64 MiB limit; metadata as renames | core 6.4, 6.5 | the commit tests of 14.1, with a crash injected after every single write; writes during a commit |
| M2.8 | Create, rename, move, delete of files and folders, folder times | core 6.3 | every row of the 6.3 table |
| M2.9 | Accept vault as found | core 7.6 | its 14.1 tests |
| M2.10 | Configuration and vault list, drive-letter search, log | core 7.1, 7.3, 7.5, 11 | 14.1 tests for 7.5; `config.yaml` read and written as `config-SAMPLE.yaml` shows |
| M2.11 | Failure tests on the in-memory file system: crashes, network errors, the power loss of the NAS, the rollback attacks of 14.3 | core 14.3 | no case returns data that fails a check as intact; every case shows the finding 6.8 names |
| M2.12 | `tools/vaultcli` (development tool): create a vault, put and get files, list, check, on a real folder, e.g. on the NAS | – | the format can be tried on the Mac mini without WinFsp; throughput of the vault layer measured there (14.4) |

**Result:** a vault can be created, filled, read, crashed and recovered on the NAS from the command line; every integrity rule of the spec is tested.

### M3 – Mounting

| # | Item | Spec | Done when |
|---|---|---|---|
| M3.1 | `internal/mount`: the WinFsp callbacks of section 12, mapped onto the vault API; volume info, the fixed security descriptor | core 6.2 step 8, 6.3, 12 | Explorer, Office and the copies of spike item 1 work on a mounted vault |
| M3.2 | Write-back cache and read-ahead in front of the vault, commit triggers (close, flush, timer, 64 MiB) | core 6.3, 6.4 | sequential throughput through the drive at least 80 % of the share's (14.4) |
| M3.3 | Network interruptions: handles kept, the *connection lost* state, dead-handle detection, re-open with the epoch check, read-only | core 6.6 | 14.3 network tests, locally and on the NAS |
| M3.4 | Unmount, also at logoff and shutdown | core 6.7 | open files, forced unmount, logoff with a mounted vault |
| M3.5 | File system tests of 14.2 (copy trees, random writes, large files, winfsp-tests, fsx) | core 14.2 | pass on a local vault and on the NAS |

| M3.6 | A `mount` CI job (O2): installs WinFsp on the GitHub runner and runs the 14.2 tests against a vault in the runner's temporary folder; a required check of `main` once it runs reliably | SPEC-ci | green on several pushes in a row; SPEC-ci and GUIDE-ci describe it |

### M4 – The program

| # | Item | Spec | Done when |
|---|---|---|---|
| M4.1 | `cmd/mountsafe`, resources (icon, manifest, version information), `build-dev.bat` and `build-release.bat` adapted from RestoreSafe | – | `MountSafe.exe` starts |
| M4.2 | `internal/gui`: widgets and Win32 helpers copied from RestoreSafe; main window, vault list and statuses | GUI 1, 2 | all statuses of GUI 2.1 shown |
| M4.3 | Unlock dialog, Create vault, Mount, Unmount, Add existing vault, Remove, Accept vault as found, problems list | GUI 3–5 | the manual checklist of GUI 6 |
| M4.4 | The `gui` CI job (a smoke test of the window), as in RestoreSafe | SPEC-ci 11 | required check of `main` |

### M5 – On the NAS, and release

| # | Item | Spec | Done when |
|---|---|---|---|
| M5.1 | Failure tests of 14.3 by hand on the Mac mini: process killed, Wi-Fi off, Sandbox as a second client, rollback attacks with real copies | core 14.3 | each case as specified |
| M5.2 | Performance on the NAS | core 14.4 | the targets of 14.4 |
| M5.3 | User README, CHANGELOG 1.0.0, release section of CONTRIBUTING.md | – | written |
| M5.4 | A self-review of the format and the crypto code instead of a paid external one (O4): a written checklist (nonces and their limits, key separation, AAD of every encryption, comparisons in constant time, zeroing, every parser's bounds, every error path of 6.8), worked through against the code, with the result kept in this plan; the user README says that MountSafe has not been reviewed independently | core 5, 6.8, 10 | checklist done, findings fixed |
| M5.5 | Release 1.0.0: from then on the vault format is frozen (CONTRIBUTING.md section 6) and the known-answer vectors never change | – | released |

## 4. Decisions

Decided by the owner, 2026-10-24:

| # | Question | Decision |
|---|---|---|
| O1 | K3: does `password_min_length` stay 12 (KDF spec 8)? | **Yes, 12**, in MountSafe and RestoreSafe (KDF spec K3). |
| O2 | A CI job that mounts: install WinFsp on the GitHub runner and run the 14.2 tests there? | **Yes, if possible** (M3.6). If it cannot run reliably on the runner, the 14.2 tests run locally before each push that touches `internal/mount`, and SPEC-ci says so. |
| O3 | SPEC-kdf in code: RestoreSafe 2.2 and MountSafe both change `cryptox` the same way (KDF 5.3). Who first? | **MountSafe in M1.3** (it has no older vaults, so no read limits below the enrollment limits); RestoreSafe 2.2 takes the same code back and adds its read limits. The two copies are compared at that point. |
| O4 | An external review of the format and the crypto code (proposal P8 of the comparison with VeraCrypt)? | **No paid review.** A written self-review instead (M5.4), and the user README says that MountSafe has not been reviewed independently. |

## 5. Risks

| Risk | Where it would show | Mitigation |
|---|---|---|
| go-winfsp has gaps or bugs in callbacks the spike did not use | M3.1 | M3.1 starts with the callbacks of section 12 one by one, each with a test; the binding is confined to `internal/mount` (D12), so a fix or a fork touches one package. |
| The commit protocol is slower than estimated on the NAS (two state writes and flushes per commit) | M2.12, M3.2 | `vaultcli` measures it in M2 already, before the mount layer depends on it; the commit cadence is a setting (`write_back_seconds`). |
| The memory of the directory index exceeds 300 bytes per entry | M2.5 | Measured with 50,000 entries in M2.5 (14.4). |
| Code copied from RestoreSafe diverges | throughout | rule 5; O3. |
