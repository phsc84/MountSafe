# Spike: checks against the target NAS

Core spec 14.5 lists assumptions about WinFsp, the Mac mini share and Windows that the vault format and the commit protocol (core spec 6.4) rely on. This program checks them, one command per item. Run it before implementation starts; record the results in the table at the end of core spec 14.5; change the specification where an assumption does not hold.

The program is a development tool (SPEC-ci 3.2): it imports nothing of MountSafe and is not shipped.

## Setup

- **PC 1:** this repository, Go, and [WinFsp](https://winfsp.dev) (for item 1 only).
- **PC 2:** a second Windows PC on the same network, for items 3 and 4. Copy `spike.exe` there; it needs no Go.
- **The share:** a folder on the Mac mini's share, mapped or as a UNC path, e.g. `\\macmini\vaults\spike`, reachable from both PCs with the same account as MountSafe will use. Nothing else should be in it.
- **The Mac:** as for MountSafe (core spec 4.1): sleep off. Item 6 turns sleep on once, on purpose.

Build once, on PC 1:

```powershell
go build -o sandbox\spike.exe ./tools/spike
$S = "sandbox\spike.exe"
$D = "\\macmini\vaults\spike"      # your share folder
```

Every command prints `PASS`, `FAIL` or `INFO` lines; copy them into the report. The exit code is 1 when a check failed.

## 1. Network drive (core spec D12, D13)

```powershell
& $S mount -src "$D\mount" -drive P:      # create the folder first; Ctrl+C unmounts
```

The drive serves `$D\mount` unencrypted, through go-winfsp, as MountSafe will serve a vault. With it mounted, check by hand and note what you see:

- Explorer shows `P:` under **Network locations** (not as a local disk).
- Copy a folder with a few hundred files in and out; compare (e.g. `robocopy /L /E` or WinMerge).
- Word and Excel: create, save, save again, close, reopen a document on `P:` (Office saves through a temporary file and renames).
- Rename and move files and folders in Explorer, also a case-only rename (`a.txt` → `A.txt`).
- Delete a file that is open in another program: Windows refuses or defers as on a local disk.
- Windows Search: `P:` is not indexed (Indexing Options don't list it).
- `fsutil fsinfo volumeinfo P:` shows file system name `MountSafe`, case-preserving and not case-sensitive.

## 2. Timestamps on the share (core spec 5.6, D10)

```powershell
& $S times -dir $D
```

Sets creation, last-write and last-access time of a file and a folder to the fixed vault time, and checks that they stay after a rename, and after a write followed by setting them again, also 5 seconds later. The last `INFO` line shows the change time: core spec 5.6 expects the server's real time there (a client cannot set it). Every line must be `PASS`.

## 3. Renames (core spec 6.3, 6.4)

On PC 1:

```powershell
& $S rename -dir $D
```

Checks that a rename onto an existing name is refused (MountSafe never replaces by renaming), that a renamed file keeps its content, a case-only rename, and the rename of a non-empty folder.

Then, atomicity as seen by a second client. On PC 1:

```powershell
& $S rename-flip -dir $D -count 3000
```

and, within 5 seconds, on PC 2:

```powershell
spike.exe rename-watch -dir \\macmini\vaults\spike -duration 60s
```

`rename-flip` renames a 1 MiB file between two names; `rename-watch` reads it under either name again and again. **"never read a partial or wrong content" must be `PASS`**: a rename that the client or server did as a copy would show half-written content. "neither name found" and "both found" counts are expected (the watcher's two reads are not one snapshot); note them. Note the time per rename.

## 4. The lock (core spec D8, 6.2)

On PC 1:

```powershell
& $S lock-hold -file "$D\vault.lock"
```

On PC 2, while it runs:

```powershell
spike.exe lock-try -file \\macmini\vaults\spike\vault.lock
```

must show `PASS` for "a second open for writing is refused" and "the holder can still be read". Then three releases, each measured on PC 2 with `-wait`, starting `lock-hold` on PC 1 again before each:

| Release | On PC 1 | Expected on PC 2 (`lock-try -wait`) |
|---|---|---|
| a. Normal end | Ctrl+C in `lock-hold` | free within seconds |
| b. Killed | `taskkill /F /PID <pid>` (the PID `lock-hold` prints) | free within seconds |
| c. Network lost | pull the network cable or turn Wi-Fi off; wait for the result; then reconnect | free after the SMB server notices; **note how long** |

For c, also note what `lock-hold` on PC 1 prints when the network comes back: whether its heartbeat fails, re-opens, or finds the lock taken by PC 2 (core spec 6.6 expects MountSafe to switch the vault to read-only then).

## 5. Throughput (core spec D4)

```powershell
& $S throughput -dir $D -size 4294967296
```

Writes and reads a 4 GiB file sequentially, then random reads and writes with 32, 64 and 256 KiB blocks for 15 seconds each, bypassing the Windows cache. Note every `INFO` line. Core spec D4 chose 64 KiB chunks: if 32 or 256 KiB are much better on this NAS, D4 is reconsidered. Run it twice; the second run counts if the first was slower.

## 6. The Mac sleeps with an open handle (core spec 6.6, 4.1)

On PC 1:

```powershell
& $S lock-hold -file "$D\vault.lock" -heartbeat 2s
```

Put the Mac to sleep (Apple menu > Sleep), wait a minute, wake it. Note what `lock-hold` prints: when the heartbeat failed, whether it worked again by itself or after re-opening, and how long it took. Then Ctrl+C.

## 7. Flushes reach the Mac's disk (core spec 6.4)

The commit order intent → data → commit holds after a power loss of the Mac only if `FlushFileBuffers` returns after the data is on its disk. macOS's `fsync` does not force the disk's own cache; whether the SMB server flushes further is what this checks.

On PC 1:

```powershell
& $S flush-write -file "$D\flush.bin"
```

It appends a 4 KiB record every 100 ms, flushes it, and shows the number of the last acknowledged record. **Pull the Mac's power cord** while it runs (not a shutdown). `flush-write` stops with a write error and prints `LAST ACKNOWLEDGED RECORD` and the flush latencies; write them down. Start the Mac again, then:

```powershell
& $S flush-check -file "$D\flush.bin" -acked <the last acknowledged record>
```

**"every acknowledged record survived" must be `PASS`.** If it fails, the commit protocol of core spec 6.4 can lose a committed state on a power loss of the NAS, and the specification is changed before implementation (for example, an extra confirmation step, or a UPS as a stated requirement). Repeat it three times; one failure counts.

## Cleaning up

Delete the folder on the share. Uninstall WinFsp from PC 1 only if you don't continue with MountSafe.
