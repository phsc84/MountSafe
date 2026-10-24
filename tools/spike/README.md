# Spike: checks against the target NAS

Core spec 14.5 lists assumptions about WinFsp, the Mac mini share and Windows that the vault format and the commit protocol (core spec 6.4) rely on. This program checks them, one command per item. Run it before implementation starts; record the results in the table at the end of core spec 14.5; change the specification where an assumption does not hold.

The program is a development tool (SPEC-ci 3.2): it imports nothing of MountSafe and is not shipped.

## Setup

- **The PC:** this repository, Go, and [WinFsp](https://winfsp.dev) (for item 1 only).
- **A second SMB client** for items 3 and 4: **Windows Sandbox** on the same PC. It is a separate Windows with its own network adapter, so the Mac sees it as a second client, like a second PC. A second PC works as well, if one is at hand.
- **The share:** a folder on the Mac mini's share, mapped or as a UNC path, e.g. `\\macmini\vaults\spike`, with the account MountSafe will use. Nothing else should be in it.
- **The Mac:** as for MountSafe (core spec 4.1): sleep off. Item 6 turns sleep on once, on purpose.

Build once:

```powershell
go build -o sandbox\spike.exe ./tools/spike
$S = "sandbox\spike.exe"
$D = "\\macmini\vaults\spike"      # your share folder
```

### Windows Sandbox as the second client

Turn it on once (PowerShell as administrator, then restart Windows):

```powershell
Enable-WindowsOptionalFeature -Online -FeatureName Containers-DisposableClientVM -All
```

Start it with `spike.exe` in reach (from the repository folder, after the build above):

```powershell
@"
<Configuration>
  <MappedFolders>
    <MappedFolder><HostFolder>$((Resolve-Path sandbox).Path)</HostFolder><SandboxFolder>C:\spike</SandboxFolder><ReadOnly>true</ReadOnly></MappedFolder>
  </MappedFolders>
</Configuration>
"@ | Set-Content sandbox\spike.wsb
Start-Process sandbox\spike.wsb
```

In the Sandbox, open PowerShell and connect to the share with the Mac account (the Sandbox has its own credentials):

```powershell
net use \\macmini\vaults /user:<mac user>
C:\spike\spike.exe lock-try -file \\macmini\vaults\spike\vault.lock
```

Everything is gone when the Sandbox window is closed. Below, "Sandbox" means this PowerShell window, "PC" the normal one.

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
- Windows Search: `P:` is not indexed. Finding a file in Explorer proves nothing (Explorer then searches file by file); the sign is the yellow bar *"Searches might be slow in non-indexed locations"* when searching on `P:`. Or ask the index (normal PowerShell, while mounted):
  ```powershell
  $c = New-Object -ComObject ADODB.Connection
  $c.Open("Provider=Search.CollatorDSO;Extended Properties='Application=Windows';")
  $r = $c.Execute("SELECT TOP 5 System.ItemPathDisplay FROM SystemIndex WHERE SCOPE='file:P:/'")
  if ($r.EOF) { "P: is not indexed" } else { while (-not $r.EOF) { $r.Fields.Item(0).Value; $r.MoveNext() } }
  ```
- In a second PowerShell, while mounted: `& $S volinfo -drive P:` checks file system name `MountSafe`, case-preserving names, no case-sensitive search, and that Windows sees a network drive. (`fsutil fsinfo volumeinfo` shows the same, but needs an administrator prompt.)

## 2. Timestamps on the share (core spec 5.6, D10)

```powershell
& $S times -dir $D
```

Sets creation, last-write and last-access time of a file and a folder to the fixed vault time, and checks that they stay after a rename, and after a write followed by setting them again, also 5 seconds later. The last `INFO` line shows the change time: core spec 5.6 expects the server's real time there (a client cannot set it). Every line must be `PASS`.

## 3. Renames (core spec 6.3, 6.4)

On the PC:

```powershell
& $S rename -dir $D
```

Checks that a rename onto an existing name is refused (MountSafe never replaces by renaming), that a renamed file keeps its content, a case-only rename, and the rename of a non-empty folder.

Then, atomicity as seen by a second client. On the PC:

```powershell
& $S rename-flip -dir $D -count 3000
```

and, within 5 seconds, in the Sandbox:

```powershell
C:\spike\spike.exe rename-watch -dir \\macmini\vaults\spike -duration 60s
```

`rename-flip` renames a 1 MiB file between two names; `rename-watch` reads it under either name again and again. **"never read a partial or wrong content" must be `PASS`**: a rename that the client or server did as a copy would show half-written content. "neither name found" and "both found" counts are expected (the watcher's two reads are not one snapshot); note them. Note the time per rename.

## 4. The lock (core spec D8, 6.2)

On the PC:

```powershell
& $S lock-hold -file "$D\vault.lock"
```

In the Sandbox, while it runs:

```powershell
C:\spike\spike.exe lock-try -file \\macmini\vaults\spike\vault.lock
```

must show `PASS` for "a second open for writing is refused" and "the holder can still be read". Then three releases, each measured on the other side with `lock-try -wait`, starting `lock-hold` again before each:

| Release | Holder (`lock-hold`) | Waiter (`lock-try -wait`) | Expected |
|---|---|---|---|
| a. Normal end | PC: Ctrl+C | Sandbox | free within seconds |
| b. Killed | PC: `taskkill /F /PID <pid>` (the PID `lock-hold` prints) | Sandbox | free within seconds |
| c. Network lost | **Sandbox**: `C:\spike\spike.exe lock-hold -file \\macmini\vaults\spike\vault.lock`, then cut its network: `Get-NetAdapter \| Disable-NetAdapter -Confirm:$false` | PC | free after the SMB server notices; **note how long** |

In c the holder is in the Sandbox, because cutting the PC's network would cut the Sandbox's too. After the result, the Sandbox's network comes back with `Get-NetAdapter | Enable-NetAdapter`; note what its `lock-hold` prints then: whether the heartbeat works again, re-opens, or finds the lock taken by the PC (core spec 6.6 expects MountSafe to switch the vault to read-only then).

**d. The same PC after a network drop** (the case with one PC). The first run (2026-10-20) showed that the Mac keeps the old open after a drop and refuses this PC's own re-open with a sharing violation. Two questions remain: does the original handle come back if it is kept open (SMB durable handles), and how long does the Mac keep the old open?

1. `& $S lock-hold -file "$D\vault.lock" -heartbeat 2s`
2. Pull the network cable or turn Wi-Fi off for about a minute, then reconnect.
3. **Leave it running** until it prints a `PASS` line, or for 10 minutes. It retries on the original handle for 3 minutes (`-keep`); if that does not come back, it re-opens and reports how long the Mac refused that.
4. Repeat with a drop of about 10 seconds.

Core spec 6.6 is changed according to the answers.

## 5. Throughput (core spec D4)

```powershell
& $S throughput -dir $D -size 4294967296
```

Writes and reads a 4 GiB file sequentially, then random reads and writes with 32, 64 and 256 KiB blocks for 15 seconds each, bypassing the Windows cache. Note every `INFO` line. Core spec D4 chose 64 KiB chunks: if 32 or 256 KiB are much better on this NAS, D4 is reconsidered. Run it twice; the second run counts if the first was slower.

## 6. The Mac sleeps with an open handle (core spec 6.6, 4.1)

On the PC:

```powershell
& $S lock-hold -file "$D\vault.lock" -heartbeat 2s
```

Put the Mac to sleep (Apple menu > Sleep), wait a minute, wake it. Note what `lock-hold` prints: when the heartbeat failed, whether it worked again by itself or after re-opening, and how long it took. Then Ctrl+C.

## 7. Flushes reach the Mac's disk (core spec 6.4)

The commit order intent → data → commit holds after a power loss of the Mac only if `FlushFileBuffers` returns after the data is on its disk. macOS's `fsync` does not force the disk's own cache; whether the SMB server flushes further is what this checks.

**First, without cutting the power: compare latencies.** Run `flush-write` for about 30 seconds and stop it with Ctrl+C; note the `FlushFileBuffers: median` line. Then, in Terminal on the Mac, in the shared folder (Perl comes with macOS; Python needs the developer tools):

```sh
perl - /Volumes/<volume>/<folder>/fsync-test.bin <<'EOF'
use strict; use IO::Handle; use Time::HiRes qw(time);
open(my $f, '>', $ARGV[0]) or die "$ARGV[0]: $!";
binmode $f;
sub measure {
    my ($sync) = @_; my @t;
    for (1 .. 200) {
        syswrite($f, 'x' x 4096) or die $!;
        my $start = time; $sync->(); push @t, time - $start;
    }
    @t = sort { $a <=> $b } @t;
    return $t[100] * 1000;
}
printf "fsync       median %.2f ms\n", measure(sub { $f->sync or die "fsync: $!" });
printf "F_FULLFSYNC median %.2f ms\n", measure(sub { fcntl($f, 51, 0) or die "F_FULLFSYNC: $!" });
close $f; unlink $ARGV[0];
EOF
```

If the flush from Windows takes less than `F_FULLFSYNC` plus a network round trip (item 5: about 0.7 ms), the SMB server does not wait for the flash, and the power cut below can only confirm a loss. On the target NAS (2026-10-24) it took 0.70 ms against 3.07 ms for `F_FULLFSYNC`; the power cut was not done, and the risk is accepted (core spec 4.1).

**The power cut**, to see what is actually lost:

On the PC:

```powershell
& $S flush-write -file "$D\flush.bin"
```

It appends a 4 KiB record every 100 ms, flushes it, and shows the number of the last acknowledged record. **Pull the Mac's power cord** while it runs (not a shutdown). `flush-write` stops with a write error and prints `LAST ACKNOWLEDGED RECORD` and the flush latencies; write them down. Start the Mac again, then:

```powershell
& $S flush-check -file "$D\flush.bin" -acked <the last acknowledged record>
```

**"every acknowledged record survived" must be `PASS`.** If it fails, the commit protocol of core spec 6.4 can lose a committed state on a power loss of the NAS, and the specification is changed before implementation (for example, an extra confirmation step, or a UPS as a stated requirement). Repeat it three times; one failure counts.

## Cleaning up

Delete the folder on the share. Uninstall WinFsp only if you don't continue with MountSafe.
