# Specification: MountSafe window application

|          |                                                                 |
| -------- | --------------------------------------------------------------- |
| Status   | Draft 0.6 (2026-10-24). Not implemented.                         |
| Core     | [SPEC-core.md](SPEC-core.md), [SPEC-kdf.md](SPEC-kdf.md) |
| Language | English only.                                                    |

This document specifies the window of `MountSafe.exe`. What the operations do is specified in SPEC-core.md; this document specifies how they are presented.

## 1. Principles

1. **Small.** One main window with three actions: **Mount**, **Unmount**, **Create vault**. Everything else is a dialog opened from these.
2. **Pure Win32**, like RestoreSafe: no WebView2, no runtime dependencies. Drawn controls, theme, and Win32 helpers are copied from RestoreSafe's `internal/gui/widget` and `internal/gui/win32`.
3. **Same unlock experience as RestoreSafe:** password field, Windows Security prompt for the YubiKey, **Use your recovery code instead**.
4. **Operable by keyboard** (access keys, Enter, Esc) and readable by screen readers, as RestoreSafe.
5. **No secrets on screen or in the clipboard** except the recovery code once at creation (copied with clipboard-history exclusion, as RestoreSafe).

## 2. Main window

```
+------------------------------------------------------------------+
| MountSafe                                                  _ [] X |
+------------------------------------------------------------------+
|  [!] WinFsp is not installed. MountSafe needs it to mount vaults. |   <- banner, only when missing (2.3)
|      Install WinFsp...                                            |
+------------------------------------------------------------------+
|  Vaults                                                           |
|  +--------------------------------------------------------------+ |
|  | Private       \\nas\vaults\private     Mounted on P:          | |
|  | Projects      \\nas\vaults\projects    Not mounted            | |
|  | Archive       \\nas\vaults\archive     In use on PC-OFFICE    | |
|  +--------------------------------------------------------------+ |
|                                                                   |
|  [ Mount... ]  [ Unmount ]  [ Open in Explorer ]                  |
|                                                                   |
|  [ Create vault... ]  [ Add existing vault... ]                   |
+------------------------------------------------------------------+
```

### 2.1 Vault list

One row per vault from `config.yaml` (SPEC-core 7.1): name, path, status. Double-click: Mount if not mounted, Open in Explorer if mounted. Context menu: Mount…, Unmount, Open in Explorer, Accept vault as found…, Remove from list…

| Status | Meaning | Mount | Unmount |
| ------ | ------- | ----- | ------- |
| Not mounted | Reachable, header copies valid, lock free. | enabled | disabled |
| Mounted on P: | Mounted by this MountSafe. | disabled | enabled |
| In use on PC-OFFICE | Lock held by another mount (tooltip: user and since when, SPEC-core 6.2). | disabled | disabled |
| Locked by an earlier session | The lock is an orphan of a session on this PC that ended while the NAS was unreachable (SPEC-core 6.2 step 2). Tooltip: *"The NAS releases it a few minutes after the connection is back."* | enabled; waits (5.2) | disabled |
| Mounted on P: – connection lost since 21:35 | The NAS is unreachable during the mount; MountSafe waits for it (SPEC-core 6.6). | disabled | enabled; drops what is not committed and says so (5.3) |
| Mounted on P: – the NAS is full | A commit failed for lack of space; 120 MB of changes are waiting, new writes are refused (SPEC-core 6.9). Tooltip: *"Free space on the NAS, or delete files on P:; MountSafe tries again every 30 seconds."* | disabled | enabled; drops what is not committed and says so (5.3) |
| Needs a newer MountSafe | A vault file has a higher format version than this MountSafe knows (SPEC-core 5). | disabled | disabled |
| Not reachable | Path not available (NAS off, drive not connected) and not found on another drive letter (SPEC-core 7.5). Tooltip names the remedy. | disabled | disabled |
| Found on E: and G: | Not at its path, but on several other drive letters (SPEC-core 7.5). | enabled; asks which (5.2) | disabled |
| Not mounted – a header copy differs | The two header copies differ, or one is unreadable (SPEC-core 7.2). Tooltip: *"MountSafe uses the intact copy when you mount and offers to repair the other."* | enabled | disabled |
| Damaged | Neither header copy is readable (SPEC-core 7.2). Tooltip names the remedy: restore `vault.msv` from a NAS snapshot or backup. | disabled | disabled |
| Read-only (lock lost) | The lock was lost during the mount, or the vault was changed by another mount while this one was cut off (SPEC-core 6.6). Tooltip names which. | disabled | enabled |

The status is refreshed at start, after every action, with `F5`, and every 30 seconds while the window is visible. When a vault is found on another drive letter (SPEC-core 7.5), the path column shows the new path; the change is in the log, no dialog.

### 2.2 Buttons

- **Mount…** (`Ctrl+M`) – 5.2.
- **Unmount** (`Ctrl+U`) – 5.3.
- **Open in Explorer** (`Ctrl+E`) – opens the drive of the selected mounted vault.
- **Create vault…** (`Ctrl+N`) – 5.1.
- **Add existing vault…** – 5.4. Needed to mount on a second PC a vault that was created on another one.

### 2.3 Banners

At most two banners at the top of the window, above the vault list:

- **WinFsp missing.** When WinFsp cannot be loaded, a banner says so and offers **Install WinFsp…**, which opens winfsp.dev in the browser. Mount is disabled; Create vault and Add existing vault stay available. The banner disappears with `F5` after installation.
- **System drive not encrypted** (SPEC-core 10.3). When BitLocker does not protect the system drive: *"This PC's system drive is not encrypted. While a vault is mounted, its keys can reach the page file or the hibernation file, and with them someone who steals this PC could open the vault."* – **How to turn on BitLocker…** (opens Settings > Privacy & security > Device encryption, or BitLocker in the Control Panel where that page does not exist) / **Dismiss** (until the next start). It does not block anything.

### 2.4 Closing the window

- No vault mounted: MountSafe exits.
- Vaults mounted: *"2 vaults are mounted (P:, Q:). Closing MountSafe unmounts them."* – **Unmount and close** / **Cancel**. Mounts live only as long as the process (SPEC-core 6.7).
- Minimizing keeps MountSafe running in the taskbar. No tray icon in version 1.
- **Logoff and shutdown** with mounted vaults: no dialog of MountSafe's own. While it commits and unmounts, Windows' "These apps are preventing shutdown" screen shows *"MountSafe is writing changes to the vault Private."* (SPEC-core 6.7); the entry disappears by itself when the vaults are unmounted.

### 2.5 Started a second time

Starting `MountSafe.exe` while it already runs for this user brings the running window to the front, restored if it was minimized; the second process ends without a window (SPEC-core 7.7).

## 3. Unlock dialog

Used by Mount (5.2). Shows the vault name and what is needed:

| Mode | Dialog |
| ---- | ------ |
| 1 Password | Password field, **Unlock**. |
| 2 Password + YubiKey | Password field, **Unlock**; then the Windows Security prompt for the YubiKey. |
| 3 YubiKey | **Unlock** starts the Windows Security prompt directly. |

- **Use your recovery code instead** (when the vault has a recovery slot): replaces the fields with one recovery code field (6 groups of 5, case and dashes ignored, checksum checked before Argon2, as RestoreSafe).
- Wrong password: *"The password is not correct."* Up to 3 attempts, then the dialog closes.
- YubiKey refused (wrong YubiKey, reset YubiKey, wrong PIN, cancelled prompt; Windows reports these alike): *"The YubiKey did not unlock this vault."* – **Try again** / **Use recovery code** / **Cancel**.
- Before Argon2 starts, the memory check of SPEC-kdf 4.4 runs. It interrupts only when the memory cannot be had (step 3, **Try again** / **Cancel**) or comes close to all of this PC's memory (step 4, **Try again** / **Continue** / **Cancel**), with the texts given there. Less free memory than the vault needs is no question: Windows makes room, and the unlock takes a few seconds longer.
- While Argon2 runs (about 4 s with the default 4 GiB on a 16 GB PC, 6–7 s while other programs use much of the memory; SPEC-kdf 6), the dialog shows *"Unlocking…"*, or *"Unlocking… Windows is making room in memory; this takes a few seconds longer."* (SPEC-kdf 4.4 step 5), and stays responsive; **Cancel** is available.
- The password field is cleared and its memory zeroed after each attempt.

## 4. Progress and errors

- Long steps (unlocking, mounting, unmounting with a cache flush) show a progress text in the vault's status cell; the window stays responsive.
- A lost connection to the NAS during a mount is not a dialog: the status cell shows *"Mounted on P: – connection lost since 21:35"* until the NAS answers again (SPEC-core 6.6). If the lock was lost for good, a message box names the unsaved changes that were lost, and the status becomes *Read-only (lock lost)*.
- Errors appear in a message box that names the vault and ends with the `Remedy:` of SPEC-core 9.
- Problems found while mounted (SPEC-core 6.8: damaged, missing, hidden entries, folders that cannot be listed) are not shown as dialogs as they occur. The vault row shows *"Mounted on P: – 3 problems"* with a **Show problems** link. It opens a list with the plaintext path, what was found (*"Changed or damaged"*, *"Deleted outside MountSafe"*, *"Added outside MountSafe – hidden"*, *"Older copy – hidden"*, *"Folder can't be listed"*) and the remedy: restore from the vault's backup, or, when the changes are known to be harmless, **Accept these changes** in the list itself (SPEC-core 7.6: only the listed entries are checked and accepted, in seconds; a confirmation first: *"Accept these 3 changes? MountSafe accepts the files and folders as they are now on the NAS, including any change someone else made to them. Damaged files stay damaged."* – **Accept** / **Cancel** (default)). **Accept vault as found** (5.6) is for when the whole vault is affected. When the list has *"Changed or damaged"*, *"Older copy"* or *"Folder can't be listed"* entries, it also says: *"If the NAS lost power while this vault was in use, this is the likely cause: the changes of its last moments did not reach its disk (SPEC-core 6.4)."* The list lives in memory only; the log names entries by plaintext path only at `log_level: debug` (SPEC-core 7.3).

## 5. Dialogs

### 5.1 Create vault

One page, checked while the user types (as RestoreSafe's Create backup window):

```
Name           [ Private                         ]
Folder         [ \\nas\vaults\private            ] [ Browse... ]
Drive letter   [ P: v ]

Unlock with    (o) Password
               ( ) Password + YubiKey
               ( ) YubiKey only

Password       [ ************                    ]
Confirm        [ ************                    ]
               At least 12 characters.

[x] Create a recovery code          (always on with a YubiKey)

The password, YubiKey and recovery code of a vault can't be changed later.
To change them, create a new vault and copy your files into it.

                                         [ Create ]  [ Cancel ]
```

Validation:

- Name: 1–64 characters of letters, digits, space, `-`, `_`, `.`, `(` and `)`, unique in the list regardless of case (SPEC-core 7.1); another character: *"Use letters, digits, spaces and - _ . ( ) only; the name also names the network drive."*
- Folder: must exist and be empty, or not exist yet (created). A folder that already contains a vault: *"This folder already contains a vault. Use Add existing vault."* A folder with only the leftovers of an interrupted creation: the question of SPEC-core 6.1 (clear it and create). A folder on a MountSafe drive: *"A vault can't be created on a drive that MountSafe serves. Choose a folder on the NAS or a local drive."*
- Drive letter: free letters only; default the first free letter from P.
- Password: `password_min_length` (SPEC-core 11), both fields equal. Hidden for mode 3.
- Recovery code checkbox: optional in mode 1, checked and disabled in modes 2 and 3.
- Memory: the vault is created with the Argon2id defaults of SPEC-kdf 4.1 (4 GiB). If this PC has less than twice that much RAM, **Create** is disabled and the window says so (SPEC-kdf 4.4). The window also notes that every PC that mounts the vault needs this memory free.

**Create** then:

1. Modes 2/3: two Windows Security prompts (register, then use the YubiKey), as RestoreSafe.
2. With a recovery code: the **Recovery code** dialog shows the code once, with **Copy** and the warning that the code alone opens the vault and must be kept in a password manager or on paper, never next to the vault. **Done** is enabled after the user ticks *"I have stored the recovery code."*
3. The vault is written (SPEC-core 6.1) and added to the list.
4. *"Vault Private created. Mount it now?"* – **Mount** / **Later**. Mount reuses the already unlocked key set; no second prompt.

### 5.2 Mount

1. If the vault was found on several drive letters (SPEC-core 7.5): *"Private was found on E:\Vaults\Private and G:\Vaults\Private. One of them may be an older copy, for example a backup."* – one button per path / **Cancel**. The chosen path is saved in `config.yaml`.
2. If the lock is an orphan of an earlier session on this PC (SPEC-core 6.2 step 2): *"Private is still locked by an earlier MountSafe session on this PC that ended while the NAS was unreachable. The NAS releases the lock a few minutes after the connection is back."* – **Wait** (default) / **Cancel**. While waiting, the status cell shows *"Waiting for the NAS to release the lock…"* and the mount continues by itself when it is free; **Cancel** stays available.
3. If the preferred drive letter is taken: a small dialog to choose a free letter (remembered in `config.yaml`).
4. Unlock dialog (3). If one header copy turns out to be damaged (SPEC-core 5.1): *"One of the two header copies of Private is damaged. MountSafe opened the vault with the intact copy. Repair the damaged copy from it?"* – **Repair** (default) / **Later** (asked again at the next mount).
5. If the last commit was interrupted (SPEC-core 6.2 step 5, 6.4), the files being written then are checked while *"Checking files from an interrupted session…"* shows in the status cell. If any is damaged: *"Private was not closed properly when it was last used. 4 files being written at that time were checked; 1 is damaged."* – **Show problems** / **OK**. If none is damaged, nothing is shown.
6. If the vault is older than when this PC last used it (SPEC-core 6.2 step 6), a warning dialog: *"This copy of Private is older than the one this PC used last (state 1,200; last used 1,234). Someone may have replaced the vault with an older copy, it was restored from a backup or snapshot on purpose, or the NAS lost power while the vault was in use and its last changes did not reach its disk. Changes made after that state are not in it."* – **Use this older state** / **Cancel** (default).
7. Mount (SPEC-core 6.2). On success the status changes to *Mounted on P:* and Explorer opens the drive.

### 5.3 Unmount

1. Flush and unmount (SPEC-core 6.7).
2. If files are still open on the drive: *"Files on P: are still open. Close them first, or unmount anyway – unsaved changes in those applications are lost."* – **Unmount anyway** / **Cancel**.

### 5.4 Add existing vault

Folder picker, then name and drive letter. MountSafe reads both header copies without unlocking (SPEC-core 7.2) and refuses folders that contain no valid vault header; a vault of a newer format is refused with *"This vault was written by a newer MountSafe. Update MountSafe to add it."* (SPEC-core 5). The vault is added to the list; nothing in the vault is changed.

If the vault is already in the list (same `vault_id`, SPEC-core 7.1) at another path: *"This vault is already in the list as Private (E:\Vaults\Private). Use the new folder for it?"* – **Use new folder** / **Cancel**. The entry keeps its name, drive letter and state counter.

### 5.5 Remove from list

*"Remove Archive from the list? The vault and its files are not changed."* – **Remove** / **Cancel**. Only for vaults that are not mounted.

### 5.6 Accept vault as found

From the context menu, for a vault that is not mounted, and offered by Mount when neither vault state slot is valid (SPEC-core 6.8). A warning dialog first:

*"Accept Private as it is now? MountSafe checks every file and accepts all changes made outside MountSafe since it was last used: hidden entries become visible again, entries reported as deleted are dropped. Do this only if you know where the changes come from, for example files you restored into the vault folder yourself. Damaged files stay damaged."* – **Accept as found** / **Cancel** (default).

Then the unlock dialog (3). Every file is read once (SPEC-core 7.6); the status cell shows *"Checking file 1,234 of 5,678…"* and **Cancel** stops without changing anything. At the end: *"Private accepted as found. 2 files are damaged."* – **Show problems** / **OK**.

## 6. Test checklist (manual)

- Create a vault in each mode, with and without a recovery code; mount; copy files; unmount; mount on a second PC via Add existing vault.
- Second PC while mounted on the first: status *In use on …*, Mount disabled.
- Wrong password three times; wrong YubiKey; recovery code with typo and correct.
- Mount with less free memory than the vault's Argon2id setting (browser and office suite open): no question, the longer progress text, the vault mounts. Mount with a page file too small for it (SPEC-kdf 4.4 step 3): the refusal appears; Try again and Cancel work.
- WinFsp uninstalled: banner, Mount disabled.
- NAS disconnected while mounted: a few seconds (nothing visible), about a minute (*connection lost* status, then normal again, nothing lost), and long enough for the lock to be lost (read-only, lost changes named).
- MountSafe ended while the NAS was unreachable, then Mount again: *Locked by an earlier session*, **Wait** mounts when the NAS releases the lock.
- Process killed during a copy: the next mount checks the interrupted files and reports only damaged ones.
- Vault replaced with an older copy (or a state slot from before the last commit put back, as a NAS power loss would leave it): the warning of 5.2 step 6; **Cancel** and **Use this older state** behave as specified, and the warning doesn't come back after accepting.
- File deleted and file added in the vault folder from outside: **Show problems** lists both with the right wording; **Accept vault as found** clears them.
- External drive connected again with another drive letter: the new path appears in the list; a second copy on another drive: *Found on E: and G:* and the choice at Mount.
- Add existing vault with a vault that's already in the list at another path.
- Close window with mounted vaults; Windows log off with mounted vaults.
- Shut down while a large copy onto P: is being committed: Windows' shutdown screen names MountSafe; waiting lets the commit finish and the vault mounts cleanly next time; **Shut down anyway** leaves an interrupted commit that the next mount recovers.
- Sleep with a vault mounted: after waking, the vault works again (or shows *connection lost* until the NAS answers). With `unmount_on_sleep: true`: the vault is unmounted before sleeping.
- NAS full during a copy: the *NAS is full* status, the copy fails with "disk full", deleting a file on P: or freeing space on the NAS lets the waiting changes through.
- System drive without BitLocker: the banner of 2.3; **Dismiss** hides it until the next start.
- `MountSafe.exe` started twice: the first window comes to the front, no second window.
- **Accept these changes** in the problem list after deleting and adding a file in the vault folder from outside: only those entries are checked, the problems disappear.
- Keyboard only: every action reachable; Narrator reads the list and the dialogs.
