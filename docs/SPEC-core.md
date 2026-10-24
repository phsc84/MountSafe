# Specification: MountSafe core

|                |                                                                                       |
| -------------- | ------------------------------------------------------------------------------------- |
| Status         | Draft 0.6 (2026-10-20). Not implemented.                                               |
| Platform       | Windows 11 64-bit only. User interface in English only.                                |
| Prerequisite   | WinFsp installed by the user (not bundled).                                            |
| Related        | RestoreSafe (phsc84/RestoreSafe). MountSafe is a separate tool; RestoreSafe needs no changes for it. Both follow SPEC-kdf.md, which also changes RestoreSafe (SPEC-kdf 5). |
| User interface | [SPEC-gui.md](SPEC-gui.md) |
| Key derivation | [SPEC-kdf.md](SPEC-kdf.md) (shared with RestoreSafe) |

MountSafe keeps files in an encrypted **vault** (a folder, typically on a NAS) and mounts it as a Windows network drive. Everything written to the drive is encrypted before it leaves the process; nothing is stored in plaintext in the vault. The cryptographic primitives, the key hierarchy and the unlock methods (password, YubiKey, recovery code) are the same as in RestoreSafe 2.x.

The current state of the vault is the truth. MountSafe keeps no versions and no history.

## 1. Goals and non-goals

### 1.1 Goals

1. **Same protection level as RestoreSafe.** AES-256-GCM, Argon2id, key sets with slots, password / password + YubiKey / YubiKey-only, recovery code. File contents, file and folder names, the folder hierarchy, attributes and timestamps are encrypted.
2. **Tamper-evident.** Any modification of a vault file's bytes, swapping contents between files, moving an entry to another folder, truncating a file, deleting or adding an entry outside MountSafe, replacing a file, a part of a file or a folder with an older copy, and replacing the whole vault with an older copy (10.1) are detected and reported; MountSafe never returns altered data as if it were intact.
3. **Robust on a NAS.** Each file is encrypted separately. A network drop or crash affects at most the files being written at that moment, never the whole vault.
4. **Standalone.** `MountSafe.exe` plus the WinFsp runtime. No account, no server, no internet connection.

### 1.2 Not supported (see section 13 for optional enhancements)

- More than one mount of the same vault at a time (also not read-only).
- Changing the password, YubiKey or recovery code of an existing vault. To change credentials, the user creates a new vault and copies the files into it. The key set of a vault never changes after creation.
- Versioning: MountSafe keeps no older versions of files. Older copies put into the vault from outside are detected (10.1), not offered as versions.
- Restoring entries deleted outside MountSafe: the deletion is detected and reported with the entry's name (6.8); the entry itself is gone.
- Alternate data streams, ACLs, symbolic links, junctions, hard links, extended attributes, EFS, NTFS compression, sparse files.
- Cloud-sync conflict handling (OneDrive, Dropbox, ...). The vault may work in a sync folder, but this is not a design target.

### 1.3 Permanently out of scope

- **Storing credentials.** As in RestoreSafe, every mount requires interactive authentication. No auto-mount at login.

## 2. Terminology

| Term            | Meaning                                                                                                        |
| --------------- | -------------------------------------------------------------------------------------------------------------- |
| Vault           | A folder containing the vault header files, the lock file and the encrypted storage tree `d\`.                  |
| Vault header    | `vault.msv` and its identical copy `vault.msv.bak`: plaintext, authenticated, written once; holds the key set.   |
| Key set, slot   | As in RestoreSafe (SPEC-core 4.4): random master key V, wrapped once per unlock method.                         |
| Entry           | A file or folder as the user sees it on the mounted drive.                                                     |
| Entry ID        | 16 random bytes per entry. For a file it is the **file ID**, for a folder the **directory ID**.                 |
| Storage folder  | The folder in `d\` that holds the encrypted entries of one plaintext folder.                                    |
| Encrypted name  | The on-disk name of an entry: its plaintext name, entry ID, attributes and timestamps, encrypted and Base32-encoded. |
| Vault time      | `created_utc` from the vault header. Every file and folder in the vault carries this time on the NAS (5.6).      |
| Chunk           | A unit of encrypted file content: 64 KiB plaintext.                                                           |
| Tag table       | The GCM tags of all chunks of a file, in order, at the end of the file entry (5.3).                            |
| Folder manifest | `folder.m` in a storage folder: the encrypted list of the folder's entries with their name and content digests (5.7). |
| Vault state     | `vault.state.a` and `vault.state.b`: the encrypted digest of every folder manifest, the state counter and the pending changes (5.8). |
| State counter   | `epoch` in the vault state; grows with every state write. Each PC remembers the highest it has seen (7.1).    |
| Commit          | One write-back cycle: intent, data, commit (6.4). After a commit, vault state, manifests and tag tables agree. |

## 3. Key design decisions

| #   | Decision | Rationale |
| --- | -------- | --------- |
| D1  | Encryption per file (Cryptomator-style), not a container file (VeraCrypt-style). | NAS-friendly: a network drop or crash damages at most the files being written; NAS snapshots work per file. No own file system inside a container. |
| D2  | Hierarchy hidden: every folder gets a random directory ID; storage folders are flat in `d\`, named by a keyed hash of the ID. | The vault reveals the number of files and folders and their approximate sizes, not names or nesting. |
| D3  | Same key set, slots, KDF and unlock methods as RestoreSafe (code copied from RestoreSafe's `internal/security`, not imported); Argon2id parameters per SPEC-kdf.md. | Proven design, same protection level. Copying needs no changes in RestoreSafe; Go `internal` packages cannot be imported across modules. |
| D4  | Chunk size 64 KiB (RestoreSafe: 8 MiB). | Files are modified in place. A small write re-encrypts one chunk; 8 MiB per write would be slow over SMB. Overhead: 44 bytes per chunk (0.07 %): nonce and tag, and the tag again in the tag table (D14). Measured on the target NAS (14.5 item 5): a small change costs one chunk read and write, about 2.5 ms at 64 KiB against 6.5 ms at 256 KiB; 32 KiB would save little and double the tag table. Sequential throughput does not depend on the chunk size, because adjacent chunks are read and written in one request (6.3, 6.4). |
| D5  | Random 96-bit nonce per chunk write, per-file key derived from the file ID. | Chunks are rewritten in place; counter nonces would repeat. Per-file keys keep the number of encryptions per key far below the AES-GCM random-nonce limit (5.4). |
| D6  | The file ID is part of the encrypted name; the file's chunks are authenticated with the file ID. | Contents cannot be swapped between files. A rename or move is a single atomic rename on the NAS; the contents are never rewritten. |
| D7  | Names are encrypted with a random nonce (non-deterministic) and looked up through an in-memory directory index. | Windows looks up names case-insensitively; a deterministic name encryption cannot match `FOO.txt` to `foo.txt`. The index is coherent because only one mount writes (D8). |
| D8  | Exactly one mount at a time, enforced by an open handle on `vault.lock` with a deny-write share mode. | No cache coherence problem, no lost updates. The handle is released by Windows/the SMB server when the process ends, so there are no stale locks to clear by hand. |
| D9  | Encrypted names use Base32 (RFC 4648, uppercase, no padding), not Base64. | The NAS share is case-insensitive; Base64 names differing only in case would collide. |
| D10 | Attributes and the true creation and last-write times are inside the encrypted name; the files on the NAS carry the vault time. | The true times are visible only on the mounted drive. A folder listing needs only the NAS directory listing, no extra read per file. A time change is one atomic rename. |
| D11 | The vault header is written once and never changed; two identical copies. | A damaged header would make the whole vault unreadable (the recovery code lives in it too). No credential changes (1.2) means no header updates and no old-header problem. |
| D12 | WinFsp through its native API binding `github.com/winfsp/go-winfsp` (no cgo). | MountSafe is Windows-only and needs Windows semantics (attributes, creation time, case-insensitive and case-preserving names, overwrite, delete-on-close, network-drive prefix); the native API provides them directly, without the POSIX translation of the FUSE layer. |
| D13 | Mounted as a network drive (WinFsp volume prefix). | Windows Search does not index network drives by default: fewer plaintext traces on the local disk. |
| D14 | Each file ends with a tag table; its digest is kept in the folder manifest. Each chunk read checks that the chunk's tag is the one in the table. | Chunk AAD alone cannot tell an old version of chunk i of the same file from the current one; the table can. Reading the table is one read per open, not one per chunk. |
| D15 | Each folder has an encrypted manifest of its entries (ID, name, digests); the vault state holds the digest of every manifest. | Detects entries deleted, added or replaced with older copies outside MountSafe, and names the deleted ones. A flat table in the vault state instead of a hash tree: one state write per commit, independent of the folder depth. |
| D16 | The vault state is written in two alternating slots with an intent before and a commit after the data (6.4); each PC remembers the highest state counter it has seen. | A crash leaves either a valid state or an intent that names the entries that may be half-written; these are accepted as found, all others are checked strictly. The remembered counter detects a whole vault replaced with an older copy, which nothing inside the vault can. |

## 4. Vault layout

```
<vault folder>\
  vault.msv              vault header
  vault.msv.bak          identical copy of the vault header
  vault.lock             lock file (D8)
  vault.state.a          vault state, slot A (5.8)
  vault.state.b          vault state, slot B (5.8)
  d\
    7Q\
      MZK3XF2A...\       storage folder of one plaintext folder (32 Base32 chars, split 2 + 30)
        folder.m         folder manifest (5.7)
        <B32>.f          file entry: file header + chunks + tag table
        <B32>.d          folder entry: empty file
        <H>.lf           file entry with a long encrypted name: file header + chunks + tag table
        <H>.ld           folder entry with a long encrypted name: empty file
        <H>.ln           the full encrypted name of <H>.lf or <H>.ld
```

- `<B32>`: the encrypted name (5.2), Base32.
- `<H>`: used when `<B32>` is longer than 220 characters: the first 32 Base32 characters of SHA-256 over the encrypted name bytes.
- Storage folder of a folder with directory ID `id`: `s = Base32(HMAC-SHA256(path_key, id))[0:32]`, path `d\<s[0:2]>\<s[2:32]>\`. The root folder has directory ID 16 zero bytes.
- Any other file or folder in the vault is ignored when mounting (and reported by a future vault check, 13.5). This includes files a macOS host creates on its own (`.DS_Store`, `._*`).

### 4.1 Target environment

The reference NAS is a Mac mini (Apple silicon) sharing an APFS volume over macOS's built-in SMB server. Relevant properties:

- APFS stores names as UTF-8 up to 255 bytes and is case-insensitive by default; encrypted names are ASCII Base32 (D9), so case sensitivity and Unicode normalization of the volume do not matter.
- APFS keeps a creation time (birth time) and a change time per file. The macOS SMB server lets a client set creation, last-write and last-access time of files and folders; the change time stays the server's (spike, 14.5 item 2).
- The macOS SMB server refuses writes through a handle opened for appending only (`FILE_APPEND_DATA` without `FILE_WRITE_DATA`, which is what Go's `os.O_APPEND` opens) with "Access is denied" (spike, 14.5 item 2; RestoreSafe 2.1.1 fixed its log for the same reason). MountSafe opens vault files with normal write access and writes at offsets; it never uses append-only handles.
- **A flush is not durable against a power loss of the Mac.** The macOS SMB server answers `FlushFileBuffers` after about an `fsync`, which hands the data to the drive but not onto its flash; `F_FULLFSYNC` would be needed for that. Measured (14.5 item 7): flush from Windows 0.70 ms median (a network round trip is about 0.7 ms), `fsync` on the Mac 0.16 ms, `F_FULLFSYNC` 3.07 ms. When the Mac loses power, writes it had confirmed in the last moments can be lost, in any order. MountSafe accepts this risk (decided 2026-10-24); a UPS for the Mac avoids it but is not required. What it costs is in 6.4 and 6.8.
- The Mac must not sleep while a vault is mounted (Energy settings: prevent automatic sleeping; `pmset -g` shows `sleep 0`). On the target NAS, the Sleep command with an SMB client connected only enters DarkWake (display off, network and file sharing active), and the mounted vault keeps working (14.5 item 6). Should the host ever sleep for real, MountSafe sees a dropped connection and handles it as 6.6 says.
- Spotlight on the Mac may index the vault folder. It sees only ciphertext; excluding the folder (Spotlight privacy list) saves resources.

A vault can also be on a local or external drive (for example a USB SSD). NTFS is recommended: exFAT works, but has no journal, so pulling the drive or a power loss during a rename can damage the drive's own file system beyond what the commit order of 6.4 covers. Pulling the drive while mounted behaves like a network interruption (6.6). External drives often get another drive letter on the next connection; 7.5 handles this.

## 5. Formats

### 5.1 Vault header

```
[8]  magic "MSVAULT\x00"
[1]  format version = 1
[3]  reserved = 0
[4]  header JSON length L (big-endian, 1 <= L <= 65536)
[L]  header JSON (UTF-8, single object; unknown fields are rejected)
[32] HMAC-SHA256(header_key, all preceding bytes)
```

| Field           | Type           | Meaning |
| --------------- | -------------- | ------- |
| `vault_id`      | b64 (16 bytes) | Random, created with the vault. |
| `created_utc`   | string         | RFC 3339, whole seconds. Also the vault time (5.6). |
| `app_version`   | string         | MountSafe version that created the vault. |
| `chunk_size`    | u32            | Must be 65536. |
| `name_encoding` | string         | Must be `"base32"`. |
| `key_set`       | object         | As RestoreSafe SPEC-core 4.4, with slot AAD prefix `"MountSafe v1 slot"` instead of `"RestoreSafe v2 slot"`. Argon2id defaults, limits and the memory check follow SPEC-kdf.md (default 4 GiB, t=2, p=4). |

Key hierarchy (RestoreSafe SPEC-core 4.4.1 up to V; Go standard `crypto/hkdf`):

```
V           = 32 random bytes, unwrapped from a slot
content_key = HKDF-SHA256(V, salt = vault_id, info = "MountSafe v1 content")
name_key    = HKDF-SHA256(V, salt = vault_id, info = "MountSafe v1 names")
path_key    = HKDF-SHA256(V, salt = vault_id, info = "MountSafe v1 paths")
header_key  = HKDF-SHA256(V, salt = vault_id, info = "MountSafe v1 header")
manifest_key = HKDF-SHA256(V, salt = vault_id, info = "MountSafe v1 manifests")
state_key   = HKDF-SHA256(V, salt = vault_id, info = "MountSafe v1 state")
file_key    = HKDF-SHA256(content_key, salt = file_id, info = "MountSafe v1 file")
```

Digests (D14, D15) are `SHA-256(...)[0:16]`, unkeyed: they are stored only inside encrypted, authenticated manifests and vault states, so an attacker would need a second preimage (2^128 work) to substitute data with the same digest.

Reading the header:

1. Read both copies. A copy is structurally valid when magic, version, reserved bytes, length and JSON fields pass validation.
2. Unlock a slot of a structurally valid copy (RestoreSafe SPEC-core 4.4: one Argon2id derivation per attempt; a wrong credential fails GCM authentication of the slot).
3. Verify the HMAC with `header_key`. A copy whose HMAC fails is damaged.
4. If both copies are valid, they must be byte-identical; otherwise the vault is reported as damaged. If one copy is damaged, use the other and offer to repair the damaged copy by copying the valid one. If both are damaged, the vault cannot be mounted (remedy: restore `vault.msv` from a NAS snapshot or backup).

The header is written only when the vault is created (6.1): `vault.msv.tmp`, flush, rename to `vault.msv`; then the same for `vault.msv.bak`.

### 5.2 Encrypted names

```
plaintext  = entry_id (16) || attributes (1) || creation_time (8) || last_write_time (8) || name (UTF-8)
aad        = "MountSafe v1 name" || parent_directory_id (16) || type (1: 'F' file, 'D' folder)
nonce      = 12 random bytes
ciphertext = AES-256-GCM(name_key, nonce, plaintext, aad)
on disk    = Base32(nonce || ciphertext) + ".f" / ".d"     (or the long form, section 4)
```

- `attributes`: bit mask of `READONLY` (0x1), `HIDDEN` (0x2), `SYSTEM` (0x4), `ARCHIVE` (stored as 0x8). Other bits must be 0.
- `creation_time`, `last_write_time`: Windows FILETIME (100 ns since 1601-01-01 UTC), big-endian. The change time reported to Windows equals the last-write time; the last-access time is not stored and reported as the last-write time.
- `name`: 1 to 255 UTF-16 code units; Windows rules for forbidden characters and reserved names apply (enforced by Windows before the request reaches MountSafe; MountSafe validates again when loading).
- The suffix type must match the type in the AAD; a renamed suffix fails authentication.
- A folder's directory ID is its `entry_id`; a file's file ID is its `entry_id`.
- Names up to about 76 UTF-8 bytes fit the short form; longer names use the long form (section 4).

### 5.3 File entry

```
+------------------------+  offset 0
| File header P (32)     |  plaintext, authenticated through every chunk's AAD
+------------------------+
| Chunk 0                |  [12] nonce | [n] ciphertext | [16] tag
| Chunk 1                |
| ...                    |
| Chunk k (final)        |
+------------------------+
| Tag table              |  (k+1) x 16 bytes: the tag of chunk 0 .. k, in order
+------------------------+
```

File header P:

```
[6]  magic "MSFILE"
[1]  format version = 1
[1]  chunk size exponent = 16
[16] file_id (must equal the entry_id in the encrypted name)
[8]  reserved = 0
```

Chunks:

- Plaintext size 65536 for every chunk except the final one (0 to 65536 bytes).
- Every file has at least one chunk. The final chunk is empty only when it is the only chunk (empty file).
- `aad = "MountSafe v1 chunk" || P (32) || chunk_index (u64, big-endian) || final (u8: 1 for the last chunk, else 0)`.
- Key: `file_key`; nonce: 12 random bytes, new on every write of the chunk.

Tag table and file digest (D14):

- The tag table repeats the GCM tag of every chunk. It is plaintext; tags are not secret, and the table is protected by its digest.
- `file_digest = SHA-256("MountSafe v1 file" || P || tag table)[0:16]`, stored in the folder manifest (5.7).
- On the first read after a file is opened, MountSafe reads the tag table (one read at the end of the file), computes the digest and compares it with the manifest. On a mismatch the file is damaged (6.8). The table stays in memory while the file is open (16 bytes per 64 KiB: 2.5 MiB for a 10 GiB file).
- Every chunk read checks, after GCM authentication, that the chunk's tag equals its entry in the table. A valid but older version of the chunk has another tag (its nonce differs) and is reported as damaged.
- Writes update the table in memory; the commit (6.4) writes the changed chunks, then the table after the final chunk, then shortens the file if it became shorter. When a file grows, new chunks overwrite the old table; the new table follows them.

Plaintext size from the encrypted file size S (no read needed; used for folder listings):

```
B = S - 32;  C = 65536 + 28 + 16
full = B / C;  r = B mod C
r == 0 and full >= 1  ->  size = full * 65536
r >= 44               ->  size = full * 65536 + (r - 44)
otherwise (incl. B < 44) -> damaged
```

### 5.4 Nonce limits

AES-256-GCM with random 96-bit nonces stays within the NIST bound up to 2^32 encryptions per key. Per key:

- `file_key`: one encryption per chunk write of that file. 2^32 chunk writes of 64 KiB are 256 TiB written into one file; not reachable in practice.
- `name_key`: one encryption per create, rename, move, attribute change or time change in the whole vault. Time changes are coalesced (6.5), so typical use stays many orders of magnitude below 2^32. A future vault check (13.5) would warn above 2^30 entries.
- `manifest_key`: one encryption per manifest write, at most one per changed folder and commit (6.4).
- `state_key`: two encryptions per commit. 2^32 would take one commit per second without pause for 68 years; commits happen only after changes, so typical use stays far below.
- Slots: as in RestoreSafe.

### 5.5 Folder entry

An empty file `<B32>.d`. Its directory ID is in the encrypted name; its storage folder is derived from it (section 4). A folder entry whose storage folder does not exist is an empty folder; the storage folder is created on the first write into it.

### 5.6 Timestamps on the NAS

Every file and folder MountSafe creates or changes in the vault (entries, `.ln` files, `folder.m`, `vault.state.a` and `.b`, storage folders and their parent folders in `d\`) gets the vault time as creation, last-write and last-access time, set with `SetFileTime` after each change.

Limits of this concealment:

- The **change time** (ctime) cannot be set by a client. The NAS file system keeps the real time of the last change of each vault file, and SMB reports it as `ChangeTime`. Windows Explorer does not show it, but anyone with access to the share can read it. It reveals when a vault file was last written or renamed, not which plaintext file it is.
- NAS snapshots, SMB audit logs and the NAS's own logs record activity independently of MountSafe.

### 5.7 Folder manifest

`folder.m` in each storage folder lists the folder's entries (D15). A folder without a storage folder (5.5) is empty and has no manifest.

```
[6]  magic "MSMANI"
[1]  format version = 1
[1]  reserved = 0
[12] nonce
[n]  ciphertext   AES-256-GCM(manifest_key, nonce, plaintext, aad)
[16] tag
aad = "MountSafe v1 manifest" || directory_id (16)
```

Plaintext: the entry count (u32, big-endian), then one record per entry, sorted by entry ID:

```
[16] entry_id
[1]  type: 'F' file, 'D' folder
[16] name_digest = SHA-256(encrypted name bytes, nonce || ciphertext before Base32)[0:16]
[16] file_digest (5.3); 16 zero bytes for a folder
[2]  name length in bytes (u16, big-endian)
[n]  plaintext name (UTF-8)
```

- The plaintext name lets MountSafe report an entry that was deleted outside MountSafe by its name (6.8).
- `manifest_digest = SHA-256(all bytes of folder.m)[0:16]`, stored in the vault state (5.8).
- A random nonce on every write; the manifest is rewritten in place (a torn write is covered by the pending list, 6.4).
- Size: about 50 bytes plus the name per entry; 50,000 entries with 30-byte names are about 4 MB, rewritten on each commit that changes the folder.

### 5.8 Vault state

`vault.state.a` and `vault.state.b` are two slots with the same layout (D16):

```
[8]  magic "MSSTATE\x00"
[1]  format version = 1
[3]  reserved = 0
[12] nonce
[n]  ciphertext   AES-256-GCM(state_key, nonce, plaintext, aad)
[16] tag
aad = "MountSafe v1 state" || vault_id (16)
```

Plaintext (integers big-endian):

```
[8]  epoch: state counter, +1 with every slot write
[4]  pending folder count, then 16 bytes per directory ID
[4]  pending file count, then 16 bytes per file ID
[4]  folder count, then per folder sorted by directory ID:
       [16] directory_id   [16] manifest_digest
```

- A slot is valid when it authenticates. The **current state** is the valid slot with the higher epoch. If neither slot is valid, the vault cannot be mounted (6.8).
- A slot write always goes to the slot that does not hold the current state, with the current epoch + 1, and is flushed before anything that depends on it is written. A torn slot write leaves the other slot as the current state.
- A state with an empty pending list is **committed**: every folder manifest has the listed digest, and every file in it matches its file digest. A state with pending entries is an **intent**: the listed folders and files may be half-written (6.4).
- Every folder with a storage folder is listed, the root folder included. Size: 32 bytes per folder; 10,000 folders are 320 KB, written twice per commit.
- Created with the vault (6.1) with epoch 1, the root folder's empty manifest, and nothing pending.

## 6. Operations

### 6.1 Create a vault

1. In the Create vault window (SPEC-gui 5.1) the user chooses an empty folder (typically on the NAS), a name, a drive letter and an authentication mode; `password_min_length` and the recovery code rules are as in RestoreSafe (SPEC-core 6.3, 6.6 there).
2. Enrollment as in RestoreSafe SPEC-core 6.3: password, YubiKey registration (own WebAuthn RP ID `mountsafe`, so RestoreSafe and MountSafe credentials are independent), recovery code (always with a YubiKey, optional in mode 1).
3. Check the available memory against the Argon2id parameters (SPEC-kdf 4.4, enrollment). Generate V and `vault_id`, create the slots and check that each unwraps before anything is written.
4. Create `d\` and the root storage folder with an empty `folder.m` (5.7); write `vault.state.a` with epoch 1 (5.8); write `vault.msv` and `vault.msv.bak` (5.1) last, so a folder with a header is always a complete vault; set the vault time on everything (5.6).
5. Add the vault to the list with its `vault_id` and the state counter 1 (7.1).

The creation dialog states that the password, YubiKey and recovery code of a vault cannot be changed later; changing them means creating a new vault and copying the files.

### 6.2 Mount

1. Open `vault.lock` (create if missing) with read/write access and share mode `FILE_SHARE_READ` and keep the handle open for the whole mount. On a sharing violation, open it read-only and read who holds it:
   - **Another PC, or a MountSafe process on this PC that is running:** *"This vault is mounted on PC-OFFICE by philip since 2026-10-20 08:12."* Nothing else happens.
   - **This PC, and no MountSafe process with that process ID is running:** the lock is an orphan of an earlier session on this PC that ended (crash, kill, power loss) while the NAS was unreachable. The NAS keeps such an open for a few minutes after the connection is back (about 3 minutes on the target NAS, 14.5 item 4) and then releases it. MountSafe says so and offers to wait: it tries again every 5 seconds and continues with step 2 as soon as the lock is free (6.6).
2. Write host name, user, process ID and time into `vault.lock` (informational only; this time is visible on the NAS, see 10.2).
3. Check the available memory against the slot's Argon2id parameters (SPEC-kdf 4.4, unlock); then read and unlock the vault header (5.1).
4. Read the vault state (5.8). If its epoch is lower than the state counter this PC remembers for the vault (7.1), the vault is older than when this PC last used it: ask before going on (6.8). **Use this older state** writes the next slot with the remembered counter + 1, so the accepted state is not reported again on this PC; **Cancel** releases the lock.
5. If the current state is an intent, recover the pending entries (6.4).
6. Remember the current epoch for this vault (7.1).
7. Check that WinFsp is installed (`winfsp.LoadWinFSP`); if not, explain how to install it (link to winfsp.dev) and release the lock.
8. Mount as a network drive (volume prefix `\mountsafe\<vault name>`) on the chosen drive letter; case-insensitive, case-preserving.

Directory index: on the first access to a folder, list its storage folder, decrypt every name, read and decrypt `folder.m`, check its digest against the vault state, and keep a map from case-folded name (Windows upper-case mapping) to entry, including attributes and times. Validation on load:

- every name authenticates, suffix and type match, no two entries with the same case-folded name, long-name pairs complete;
- every entry on disk is in the manifest with the same entry ID, type and name digest;
- every entry in the manifest is on disk.

A failing entry is hidden and reported (6.8); the folder stays usable. A folder whose manifest is missing, fails authentication or does not match the digest in the vault state cannot be listed (6.8).

### 6.3 File system operations

| Windows request | Vault action |
| --------------- | ------------ |
| Create file | New file ID; write `<B32>.f` with header and one empty final chunk; add to index. |
| Create folder | New directory ID; create the storage folder, then `<B32>.d` (a crash in between leaves only an orphan storage folder). |
| Open | On the first read: read the tag table and check the file digest (5.3). |
| Read | Read the chunks covering the range with **one** request over their contiguous bytes (never one request per chunk), decrypt them and check each tag against the tag table; any failure returns `STATUS_FILE_CORRUPT_ERROR` and is reported (6.8). When a file is read sequentially, read ahead up to 1 MiB, so that small requests from Windows become large requests to the NAS. |
| Write | Read-modify-write the affected chunks into the write-back cache (6.4). Extending a file re-encrypts the previous final chunk as non-final. Updates the last-write time in the index. |
| Overwrite (`CREATE_ALWAYS`, `TRUNCATE_EXISTING`) | Truncate to one empty final chunk; apply the requested attributes. |
| Set file size | Re-encrypt the new final chunk; shorten or extend the encrypted file. |
| Rename / move | Encrypt the name with the new parent directory ID, attributes and times, rename the encrypted file (one SMB rename; the entry ID stays). Long names: write the new `.ln`, rename the content file, delete the old `.ln`. |
| Set attributes / set times | Update the index; written as a rename with the new encrypted name (6.5). `READONLY` is enforced by MountSafe. |
| Delete file | Delete the encrypted file (and `.ln`). Delete-on-close is honoured. |
| Delete folder | Only when empty (Windows semantics): delete the storage folder, then the folder entry. |
| Folder times | Creating, deleting or renaming an entry updates the parent folder's last-write time, as NTFS does (coalesced, 6.5). |
| Free space | `GetDiskFreeSpaceEx` of the vault folder. |
| Not supported | Alternate data streams, reparse points, hard links, extended attributes, security descriptor changes (a fixed descriptor grants the current user full access). |

Every change also updates the folder manifests concerned (a move changes two) and, with them, the vault state; both are written by the next commit (6.4). A deleted folder is removed from the vault state.

### 6.4 Write-back cache, commits and crash consistency

- Modified chunks, names and metadata are kept in memory and written by a **commit**: when a file is closed, on `FlushFileBuffers`, on unmount, at most `write_back_seconds` (default 5) after the change, and as soon as the modified chunks in memory reach 64 MiB. One commit covers all changes collected since the previous one. While a commit for the 64 MiB limit runs, further writes wait, so a large copy cannot fill the memory: it runs at the speed of the NAS.
- The data step writes each run of adjacent modified chunks of a file with **one** request, not one per chunk; on the target NAS one 64 KiB request costs about 1.25 ms, so chunk-by-chunk writing would halve the sequential throughput (14.5 item 5).
- Plaintext is never written to a temporary file or anywhere outside the vault.

A commit has three steps (D16):

1. **Intent.** Write the next state slot (5.8): the folder table unchanged, the pending list naming every file whose content changes and every folder whose manifest changes (the folder of a changed file is always among them). Flush.
2. **Data.** Write the chunks and tag tables of the pending files; create, rename and delete entries; write the manifests of the pending folders; set the vault time (5.6). Flush.
3. **Commit.** Write the other slot: epoch + 1, the new folder table, nothing pending. Flush.

After a crash, a lost connection or a killed process, the current state is either the last commit (the intent was torn, nothing else written) or the intent. At the next mount (6.2), an intent is **recovered**:

- Each pending folder: its manifest is rebuilt from the entries on disk whose names authenticate.
- Each pending file: every chunk is read and authenticated, and the tag table and file digest are rebuilt from the chunks. A file with a chunk that fails authentication stays damaged (6.8).
- Then a commit with the rebuilt manifests and nothing pending.
- Entries outside the pending list are checked strictly as always. A recovery that found damaged files is reported (6.8); otherwise it is only logged.

- Ordering rules within the data step guarantee that a crash or network drop never leaves an entry that authenticates with wrong content:
  - Create folder: storage folder first, then the folder entry.
  - Delete folder: storage folder first, then the folder entry.
  - Long-name rename: new `.ln` first, then the content file, then delete the old `.ln`.
  - Content before metadata: chunks and the tag table are written before the rename that records the new last-write time.
  - Entries before manifests: a manifest is written after the entries it lists.
- Interrupted chunk writes: the affected file may fail authentication afterwards. It is reported as damaged, never returned as intact; other files are not affected. Applications that save by writing a temporary file and renaming it (Office, most editors) are not affected.

**What the commit order covers.** The steps and ordering rules above rely on a flush returning only after the data is durable. That holds for a crash of the PC or of MountSafe and for network interruptions: the NAS keeps running and writes out what it confirmed. It does not hold for a **power loss of the NAS host** on the target NAS (4.1): writes the NAS confirmed in its last moments can be lost, in any order. Then the vault on disk may hold a state slot older than the last commit, chunks, tag tables, names or manifests from before their last change, or a mix. MountSafe still never returns such data as intact: every mismatch is found by the checks of 6.8. But it cannot tell a power loss from someone putting older copies in place, so it reports both alike, names a power loss of the NAS as a possible cause, and the changes of the last moments before the power loss are lost. **Accept vault as found** (7.6) makes the vault usable again with what is on disk.

### 6.5 Metadata updates

Attribute and time changes are kept in the directory index and written by the next commit (6.4) as one rename per entry: when the file is closed (Windows cleanup), on flush, on unmount, and at most `write_back_seconds` after the change. A file copied in by Explorer (create, write, set times, set attributes, close) therefore costs one create and one rename. After each write the vault time is set on the changed vault files (5.6).

A crash before the rename loses only the metadata change: the entry keeps its previous attributes and times.

### 6.6 Network interruptions

Behaviour of the target NAS, measured in the spike (14.5 item 4):

| Drop | What happens |
| ---- | ------------ |
| A few seconds | The Windows SMB client hides it; no request fails. Writes may only reach Windows' cache in the meantime; they are on the NAS only after a flush returns. |
| About a minute, handles kept open | Requests fail with network errors while the NAS is unreachable. When it is back, Windows reattaches the open handles (SMB durable handles); the lock stays held throughout, and no other client can take it. |
| A handle closed, or the process ended, during the drop | The NAS keeps the open until its durable timeout, about 3 minutes after the connection is back, and refuses every other open of the file in that time, also from this PC. |
| The connection is closed or reset rather than silent (seen when a client's network adapter was switched off behind NAT) | The NAS drops the client's session and its opens at once: another client can take the lock within seconds, and the client's handles never reattach; they fail with "An unexpected network error occurred" after the network is back. |

Therefore:

- **Never close a vault handle because of a network error.** MountSafe keeps the lock handle and every other open vault handle through a drop; closing one would leave an orphaned open on the NAS (third row) and make the lock unavailable even to this PC. Handles are closed only when the file is done with, or at unmount.
- **Durability comes from flushes only.** A write that returned is not on the NAS until a flush has returned; every step of a commit ends with a flush (6.4), so a drop of any length never makes a commit look complete when it is not.
- **During a drop,** reads that the cache cannot serve and commits fail with network errors; requests from Windows get I/O errors, data in the write-back cache stays in memory. The vault's row shows *"Connection lost since 21:35 – waiting"* (SPEC-gui 4). MountSafe retries the failed commit when the NAS answers again, as long as the user lets it wait; **Unmount** during a drop drops what is not committed and says which plaintext paths are affected.
- **When the NAS answers again** (a listing of the vault folder works) and the lock handle works, the mount continues as if nothing happened. If the lock handle still fails then, it is dead: the NAS dropped the session (last row) or the drop outlasted its durable timeout. MountSafe does not wait for it but re-opens the lock at once:
  - **Re-opened:** nobody took the vault in the meantime. MountSafe writes its holder line into the lock again, re-opens the other vault files whose handles are dead, and repeats the failed commit. Nothing is lost.
  - **Refused, or failing otherwise:** another PC may have mounted the vault. The volume switches to read-only and MountSafe tells the user which unsaved changes were lost (plaintext paths). An interrupted commit is recovered at the next mount (6.4).
- **A session that ends during a drop** (crash, kill, power loss) leaves an orphaned lock on the NAS for a few minutes; the next mount on this PC recognises it and waits (6.2 step 1).

### 6.7 Unmount

Commit the cache and pending metadata (6.4, 6.5), unmount the volume, remember the final epoch (7.1), close the lock handle, and zero V and all derived keys. Unmount is also done when the user logs off or shuts down Windows. If a file is still open, Windows asks the application; a forced unmount drops unsaved changes and says so.

### 6.8 Detected changes and how they are reported

| Finding | Detected | Effect |
| ------- | -------- | ------ |
| Bytes of a name, chunk, header, manifest or state changed | On access (GCM, HMAC) | Entry hidden or `STATUS_FILE_CORRUPT_ERROR`; header and state: below. |
| Old version of a chunk in a file (D14) | On read: tag differs from the tag table | `STATUS_FILE_CORRUPT_ERROR` for that range. |
| Old version of a whole file, or its tag table changed | On open: file digest differs from the manifest | `STATUS_FILE_CORRUPT_ERROR` on every read of the file. |
| Entry deleted outside MountSafe | On folder access: in the manifest, not on disk | Reported as missing with its name from the manifest. |
| Entry added outside MountSafe (including an old copy of a deleted entry) | On folder access: on disk, not in the manifest | Hidden. |
| Old name, attributes or times of an entry | On folder access: name digest differs | Hidden. |
| Folder manifest missing, damaged or older | On folder access: digest differs from the vault state | The folder cannot be listed (`STATUS_FILE_CORRUPT_ERROR`); its entries are not shown. |
| Whole vault replaced with an older copy | On mount: state counter lower than this PC remembers (7.1) | Question before mounting (6.2). |
| Interrupted commit | On mount: the current state is an intent | Recovery (6.4); files found damaged are reported. |
| Neither state slot valid | On mount | The vault cannot be mounted; remedy: restore `vault.state.a` and `.b` from a snapshot or backup, or **Accept vault as found** (7.6). |
| Power loss of the NAS host while the vault was in use (6.4) | Not as such: it shows as some of the findings above (older state, older or damaged files, folders that cannot be listed) for what was written in the last moments | As those findings. Every report of an older state or older copies says that a power loss of the NAS is a possible cause (SPEC-gui 4, 5.2). |

- Every finding of a mount session is kept in a problem list in memory, with plaintext paths, and shown in the window (SPEC-gui 4). The log records findings by encrypted name, storage folder or entry ID; plaintext paths only at `log_level: debug` (7.3).
- Hidden entries and folders that cannot be listed stay untouched in the vault; nothing found is deleted automatically. Writing into a folder that cannot be listed is refused.
- The remedy for hidden or missing entries is the vault's backup, or **Accept vault as found** (7.6) when the change is known to be harmless (for example files restored into the vault folder by hand).

## 7. Further functions

### 7.1 Vault list

MountSafe keeps a list of known vaults in `config.yaml` next to the executable or at `-config`: display name, path, `vault_id` (read from the header without unlocking), preferred drive letter, and the highest state counter this PC has seen (`seen_state`, 6.2 and 6.7). It contains no secrets. The GUI maintains the list: a created vault is added automatically, an existing vault (for example created on another PC) is added with **Add existing vault…** (SPEC-gui 5.4). Adding a folder whose `vault_id` is already in the list updates that entry's path instead of adding a second entry.

`seen_state` is what detects a vault replaced with an older copy (6.8). It works only on PCs that have used the newer state, and it is not protected against someone who can edit `config.yaml` on this PC (10.2).

### 7.2 Startup check (no password)

For each known vault: reachable, both header copies structurally valid and identical, `vault_id` as in the list, `vault.lock` free or held (and by whom), WinFsp installed. A vault that is not found at its path is searched for on other drive letters (7.5).

### 7.3 Log

One log file per mount session in a log folder next to the executable (not in the vault). Plaintext paths appear in the log only at `log_level: debug`; at the default level damaged entries are logged by their encrypted name and storage folder.

### 7.4 Damaged entries in version 1

Version 1 has no separate vault check (13.5). Damaged, missing and added entries are found when their folder or file is accessed (6.8). Orphans left by a crash (storage folders without a folder entry, `.ln` files without a content file) are harmless and ignored.

### 7.5 Vault on another drive letter

An external drive often gets another drive letter when it is connected again, so a vault at `E:\Vaults\Private` may be at `F:\Vaults\Private` next time.

1. When the path of a vault is a drive-letter path and the folder there is missing or holds a vault with another `vault_id`, MountSafe looks for the same path without the drive letter on every other drive letter (local and removable drives).
2. A candidate matches when both header copies are structurally valid and identical and their `vault_id` equals the one in the list (7.1).
3. **One match:** MountSafe uses it, updates the path in `config.yaml`, logs *"Vault Private found on F: (was E:)."* and shows the new path in the list.
4. **Several matches** (for example the vault and a copy of it on a backup drive): nothing is chosen automatically. The list shows *"Found on E: and G:"* and Mount asks which to use. If an older copy is chosen, the state counter check (6.2) reports it on a PC that has used the newer one.
5. **No match:** the vault is *Not reachable*; remedy: connect the drive, or use **Add existing vault…** with the vault's new folder (7.1).

A vault on a network path (`\\nas\...`) is not searched for; when the NAS or share is renamed, **Add existing vault…** updates the path. A matching `vault_id` only identifies the vault; it proves nothing: a folder with a copied header still has to be unlocked and passes all checks of 6.8.

### 7.6 Accept vault as found

For a vault that is unlocked and not mounted (SPEC-gui 5.6): rebuild every folder manifest from the entries on disk whose names authenticate, rebuild the tag table and file digest of every file from its chunks, and write a new committed state with the state counter above the one this PC remembers. Every file is read once.

- Entries that were hidden because they were added outside MountSafe or carry an older name become visible again; entries reported as missing are dropped from the manifests.
- Files with a chunk that fails authentication stay damaged.
- Used when neither state slot is valid, or when the user knows that the reported changes are harmless. The confirmation says that any change made by someone else since the last commit is accepted with it.

## 8. Dependencies and project layout

- Go standard library, `golang.org/x/crypto/argon2`, `golang.org/x/sys/windows`, `github.com/winfsp/go-winfsp` (MIT; no cgo, loads the WinFsp DLL at runtime). The go-winfsp version is pinned and its use is confined to `internal/mount` (D12).
- Runtime: WinFsp (GPLv3 with FLOSS exception; MountSafe is GPL-3.0 like RestoreSafe).

| Folder | Content |
| ------ | ------- |
| `cmd/mountsafe` | Entry point of `MountSafe.exe`. |
| `internal/gui` | The window (SPEC-gui). |
| `internal/mount` | WinFsp adapter (go-winfsp native API): maps file system requests to `vault` operations, write-back cache. The only package that imports go-winfsp. |
| `internal/vault` | Vault format: header, names, file entries and tag tables, folder manifests, vault state and commits, directory index, lock. No WinFsp dependency. |
| `internal/security` | Copied from RestoreSafe: `cryptox`, recovery codes, YubiKey through Windows WebAuthn; slot AAD prefix and RP ID changed; Argon2id defaults, limits and memory check per SPEC-kdf.md. |
| `internal/config`, `logging` | Configuration, log files. |

Imports point downward only (`gui` → `mount` → `vault` → `security`); checked by an architecture test as in RestoreSafe.

## 9. Error handling principles

- Every error names the vault and, where known, the plaintext path, and ends with a `Remedy:`, as in RestoreSafe.
- Never return data that failed authentication; never show a damaged entry under a guessed name.
- Never fall back silently: a lost lock, a damaged header copy, or a dropped cache flush is shown to the user.
- Key material is zeroed after use.

## 10. Security considerations

Threat model: the attacker has the vault's files but not its credentials: a copy of the vault, NAS snapshots or backups of it, or write access to the share or drive. They have unlimited time offline and can change, delete, add and replace vault files. The first goal is that they learn nothing beyond 10.2 (confidentiality); the second, that every change they make is detected and reported (integrity, 6.8). An attacker with write access can always destroy data; MountSafe detects that, the vault's backup repairs it. Attacks on the PC while a vault is unlocked or mounted (malware, memory dumps, other processes reading the drive) are outside this model (10.2), as in SPEC-kdf section 1.

### 10.1 Protected

- File contents, file and folder names, folder hierarchy, attributes, creation and last-write times: encrypted (AES-256-GCM).
- Modification of any byte of a header, name, chunk, tag table, manifest or vault state; swapping file contents between entries (file ID in name and chunk AAD); moving an entry into another folder (parent directory ID in the name AAD); changing an entry's type; truncating a file at any position (final flag in the chunk AAD); reordering chunks (chunk index in the AAD).
- Replacing data with an older valid copy: a chunk (tag table, D14), a whole file or an entry's name, attributes and times (folder manifest, D15), a folder's manifest (vault state, D15), and the whole vault on a PC that has used the newer state (state counter, D16).
- Deleting or adding entries outside MountSafe (folder manifest, D15); deleted entries are reported by name.
- Guessing: Argon2id with the parameters of SPEC-kdf.md (default 4 GiB, offline threat model); with a YubiKey a password alone is not enough.
- Adding a slot controlled by an attacker: impossible without V; any header change fails the HMAC.

### 10.2 Visible or not detected (known limitations)

- Visible to anyone with access to the NAS: the number of files and folders and which storage folder holds which entries (but not how the folders nest), the approximate size of each file (±44 bytes per 64 KiB), which entries are files and which folders, and the change time (ctime) of each vault file (5.6), including when a commit last changed `vault.state.*` and a folder's `folder.m`.
- Replacing the whole vault with an older copy is detected only on a PC that has used a newer state (7.1). A PC that never saw the newer state (a new PC, a reinstalled one, a `config.yaml` without `seen_state`) accepts the older copy. Someone who can edit `config.yaml` on the PC can lower `seen_state`.
- After an interrupted commit, the pending entries are accepted as found at the next mount (6.4). An attacker who also controls the moment of the crash, or who has an intent from an earlier interrupted commit and a snapshot that matches it, could put older copies of exactly those entries in place. Committed states never accept anything without checking.
- **Accept vault as found** (7.6) accepts every change made since the last commit, including an attacker's.
- A power loss of the NAS host can lose the last confirmed changes (4.1, 6.4). The reports it causes look like a rollback by an attacker, and MountSafe cannot tell them apart; after a known power loss, **Accept vault as found** is the usual remedy, and it would also accept an attacker's change made at the same time.
- `vault.lock` holds the host name, user and mount time in plaintext (6.2).
- Plaintext outside the vault: while mounted, every process of the user can read the drive. Windows and applications may keep plaintext traces on the local disk (thumbnail cache, recent-files lists, application temp and autosave files, page file). This is the same for VeraCrypt.
- The Argon2id working memory is not wiped after unlocking (`x/crypto/argon2` has no option for it) and may reach the page file.

## 11. Configuration

```
# Known vaults (maintained by the GUI)
#   vault_id:   from the vault header; finds the vault again when its drive letter changes
#   seen_state: highest state counter this PC has seen; detects an older copy of the vault
vaults:
  - name: "Private"
    path: "\\\\nas\\vaults\\private"
    vault_id: "3q2+7wAAAAAAAAAAAAAAAA=="
    drive_letter: "P"
    seen_state: 1234

# Minimum password length (in characters) when a vault is created. At least 8.
# The authentication mode and the recovery code are chosen in the Create vault window.
password_min_length: 12

# Seconds after a change at which modified data and metadata are written to the vault at the latest
write_back_seconds: 5

log_level: "info"
```

## 12. Implementation notes for go-winfsp

- Mount options: `VolumePrefix` (network drive, D13), `CaseSensitive(false)`, `CasePreserveNames(true)`, `FileSystemName("MountSafe")`.
- Callbacks used: `GetVolumeInfo`, `GetSecurityByName`, `Create`, `Open`, `Overwrite`, `Cleanup`, `Close`, `Read`, `Write`, `Flush`, `GetFileInfo`, `SetBasicInfo`, `SetFileSize`, `CanDelete`, `Rename`, `GetSecurity`, `ReadDirectory`, `GetDirInfoByName`.
- Not implemented (feature not supported): reparse points, extended attributes, `SetSecurity`, streams.
- Security descriptor: one fixed descriptor (owner and full access for the current user, built with the `procsd` helper), returned for every entry.
- File info timeouts: short (e.g. 1 s), since only MountSafe changes the vault but metadata is written lazily (6.5).

## 13. Optional future enhancements (not implemented)

Deletion detection per folder, formerly listed here, is part of version 1 (folder manifest, 5.7).

### 13.1 Size padding

Pad files to size classes to hide exact sizes, at the cost of storage.

### 13.2 Read-only second mounts

Allow additional read-only mounts while one mount writes; needs change notification or short cache timeouts.

### 13.3 Auto-unmount

Unmount after inactivity, on screen lock, or on sleep.

### 13.4 Copy into a new vault

A function that creates a new vault with new credentials and copies all files from the mounted vault, for the credential change described in 1.2.

### 13.5 Check vault

With the vault unlocked and not mounted: decrypt every name and every chunk of every file, check every tag table, folder manifest and the vault state, and report damaged names, files, manifests, state slots and header copies, and missing and added entries (6.8); orphans (storage folders without a folder entry, `.ln` files without a content file and vice versa, unknown files); duplicate entry IDs. Orphans can be removed after a confirmation that lists them; damaged files are never removed automatically.

## 14. Test plan

### 14.1 Unit tests

- Vault header: encode/decode, every field validated, unknown fields rejected, HMAC failure on any changed byte, both copies, differing valid copies rejected, one damaged copy, both damaged.
- Slots: as RestoreSafe (all slot types, wrong credential, slot swap, AAD prefix differs from RestoreSafe so slots cannot be moved between the tools); Argon2id enrollment and read limits and the memory check (SPEC-kdf 4).
- Names: round trip including attributes and times, wrong parent directory ID, wrong type suffix, unknown attribute bits, Base32 only, long-name threshold and pair handling, 255 UTF-16 code units, case-insensitive duplicates rejected.
- File entries: size formula (with tag table) for 0, 1, 65535, 65536, 65537 and large sizes; invalid sizes; chunk swap, reorder, truncation at and between chunk boundaries, extension with a forged chunk, file ID mismatch between name and header, header byte changes; random nonce per rewrite.
- Tag table: an older valid version of one chunk is rejected on read; an older version of the whole file, a changed or truncated table, and a table from another file fail the file digest; the table grows and shrinks correctly with the file.
- Folder manifest: round trip; entry deleted, added, renamed back to an older name, and replaced with an older copy are each detected and reported as specified in 6.8; deleted entries are reported with their name; manifest from another folder (AAD) rejected; older manifest rejected through the vault state.
- Vault state: both slots, torn write of either slot, current slot is the valid one with the higher epoch, neither slot valid, state from another vault (AAD) rejected; state counter lower than `seen_state` asks, equal or higher does not; **Use this older state** sets the counter above `seen_state`.
- Commits: a crash injected after every single write of a commit (intent, each data write, commit); after recovery, every entry outside the pending list is unchanged and checked strictly, pending entries are accepted as found, and no committed state is ever lost.
- Accept vault as found (7.6): hidden entries return, missing ones are dropped, damaged files stay damaged.
- Vault on another drive letter (7.5): one match updates the path; several matches ask; a folder with the same path but another `vault_id` is not used; **Add existing vault…** with a known `vault_id` updates the path.
- Directory index: lookup by any case, rename, move, delete, damaged entry isolated, metadata coalescing.
- Timestamps on the NAS: every vault file and storage folder carries the vault time after create, write, rename and delete.
- Lock: second mount refused and shows the holder; lock released when the process is killed.
- Fuzz tests for the header JSON and the name, chunk, tag table, manifest and vault state parsers.

### 14.2 File system tests (mounted, scripted)

- Copy a tree in and out and compare byte for byte, including times and attributes.
- Random-access writes, appends, truncations, overwrite (`CREATE_ALWAYS`), save-via-temp-and-rename as Office does.
- Rename and move of files and folders with long names; case-only renames.
- Large files (> 4 GiB, > 100 GiB).
- Concurrent reads and writes from several processes.
- winfsp-tests and fsx against the mounted drive, as cgofuse and WinFsp use them.

### 14.3 Failure tests

- Process killed during writes: only the affected files are reported as damaged; all other files read correctly after the next mount.
- Power loss of the NAS host, simulated below the vault layer by a file system that drops a random subset of the last confirmed writes (and tears some of them) before the next mount: no data is ever returned as intact when it is not; every lost write shows as a finding of 6.8; **Accept vault as found** makes the vault usable again.
- NAS connection dropped and restored during writes: for a few seconds (nothing visible), for about a minute (handles reattach, the failed commit is retried, nothing lost), and longer than the NAS's durable timeout or with the session dropped by the NAS (lock re-opened, handles re-opened, the failed commit repeated, nothing lost); lock lost and taken by another PC in between (read-only, lost changes named).
- MountSafe killed during a drop: the next mount on this PC recognises the orphaned lock, waits, and mounts when the NAS releases it (6.2 step 1).
- Damaged `vault.msv` with intact `vault.msv.bak` and vice versa.
- Bit flips in names, headers, chunks, tag tables, manifests and state slots are reported, never returned.
- Rollback attacks with real snapshots: take a copy of the vault, keep working, then put back from the copy one chunk, one file, one `folder.m`, one state slot, both state slots, and the whole vault; each is detected as in 6.8.
- External drive: pulled while mounted (6.6, recovery at the next mount, 6.4); connected again with another drive letter (7.5); a vault on exFAT.

### 14.4 Performance

On the target NAS: sequential read/write throughput through the mounted drive (target: at least 80 % of the raw throughput of the share, 14.5 item 5: about 80 MiB/s on gigabit Ethernet), small-file creation rate, folder listing with 50,000 entries, memory of the directory index (target: under 300 bytes per entry). The cost of commits (two state writes, the manifests of the changed folders, flushes): copying 10,000 small files into one folder, and into 10,000 folders; small random writes into a 10 GiB file (tag table rewrite). Opening a 100 GiB file (tag table read, 25 MiB).

### 14.5 Spike before implementation (target NAS, 4.1)

With the go-winfsp `memfs` or passthrough example and small test programs against the Mac mini share:

1. Mount as a network drive with `VolumePrefix`; Explorer, Office save, copy and rename behave normally.
2. `SetFileTime` on a file and a folder on the share sets creation, last-write and last-access time, and they survive a later write and rename (5.6).
3. A rename on the share replaces nothing and is atomic as seen by a second client.
4. `vault.lock` opened with share mode `FILE_SHARE_READ` blocks a write-open from a second SMB client (a second PC, or Windows Sandbox on the same PC), and is released when the holding process is killed and when the holder loses the network (D8). After a network drop of the holder, it can open the lock again when nobody else took it (6.6).
5. Throughput of random 64 KiB reads and writes vs. 32 KiB and 256 KiB (D4).
6. Behaviour when the Mac sleeps and wakes during an open handle (6.6).
7. `FlushFileBuffers` on a file on the share returns only after the data is on the Mac's disk, so the order intent → data → commit (6.4) holds after a power loss of the Mac.

Any assumption that does not hold is changed in this specification before implementation starts.

The programs for these checks are in `tools/spike`; its README says how to run each. Results (filled in after the spike):

| Item | Result | Consequence for this spec |
| ---- | ------ | ------------------------- |
| 1. Network drive, Explorer, Office | 2026-10-20, go-winfsp v1.0.6: network location; copy, Office save, rename (also case-only), delete of an open file as on a local disk; `volinfo`: file system `MountSafe`, case-preserving, not case-sensitive, `DRIVE_REMOTE`; not in the Windows Search index. | None: D12 and D13 hold. |
| 2. Timestamps on the share | 2026-10-20: creation, last-write and last-access time of a file and a folder set to the vault time; unchanged after a rename, and after a write followed by setting them again, also 5 s later. The change time is the server's real time of the last change, including the last SetFileTime. Writes through an append-only handle are refused ("Access is denied"). | 5.6 holds. 4.1: vault files are never opened append-only. |
| 3. Renames: refused onto existing, atomic for a second client | 2026-10-20, one client: a rename onto an existing name is refused and leaves it unchanged; a renamed file keeps its content and its old name is gone; case-only rename; rename of a non-empty folder. 2026-10-24, second client (Windows Sandbox, SMB client caches off): 5000 renames of a 1 MiB file at 9.6 ms each; the second client read it 1237 times under either name, never partial or wrong. | None: renames are single operations on the NAS (6.4 holds). |
| 4. Lock: refused, released on end / kill / network loss (seconds), re-opened after a drop | 2026-10-20, d (one PC, Wi-Fi off about 1.5 min): after reconnecting, the Mac refused this PC's own re-open of the lock with a sharing violation (it still held the old open), for at least 22 s, until stopped. Second run, Wi-Fi off about 1 min, the original handle kept open: the heartbeat worked again on it after 1 min 10 s (SMB durable handle reconnected); the refusal of the first run came from closing the handle during the drop. Third run, about 10 s off: no error at all. Fourth run, the holder ended during the drop: the Mac refused the lock to this PC from the reconnect (21:36:20) for 3 min 1 s, then released it. 2026-10-24, a–b (second client: Windows Sandbox): its write-open is refused with a sharing violation while this PC holds the lock, and it reads the holder; the lock became free for it after a normal end and after `taskkill /F` of the holder (11 s and 44 s after its first refusal, including the time until the holder was stopped by hand). c: the Sandbox held the lock and its adapter was switched off: the lock was free for this PC 15 s later, and the Sandbox's handle never reattached ("An unexpected network error occurred"); a re-open worked once the lock was free. The NAS treats a closed or reset connection differently from a silent one (d). | **6.6 rewritten**: handles are kept through a drop, never closed because of a network error; durability only from flushes. **6.2 step 1**: a lock left by an ended session of this PC is recognised and waited for. **6.6**: a handle that still fails when the NAS answers again is dead; the lock is re-opened at once, and on success the mount continues without loss. |
| 5. Throughput 32 / 64 / 256 KiB | 2026-10-24, two runs, Windows cache bypassed: sequential 1 MiB write 98–101 MiB/s, read 87–95 MiB/s (gigabit Ethernet is the limit). Random read/write: 32 KiB 1112–1125 / 984–1054 ops/s (31–35 MiB/s); 64 KiB 795–837 / 731–765 ops/s (46–52 MiB/s); 256 KiB 315–316 / 291–318 ops/s (73–80 MiB/s). A request costs about 0.7 ms plus its transfer time. | D4 holds (64 KiB). New: reads and writes of adjacent chunks in one request, read-ahead up to 1 MiB (6.3, 6.4); performance target in 14.4. Also: a commit starts when 64 MiB of modified chunks are in memory (6.4). |
| 6. Mac sleep with an open handle | 2026-10-24: Sleep from the Apple menu while this PC held the lock: the Mac entered DarkWake for 151 s (`TCPKeepAlive=active`; `sleep 0`, sleep prevented by powerd; `womp 1`), file sharing went on serving, and no heartbeat failed. A real sleep could not be produced with a client connected. | 4.1 states it. Nothing else: a real sleep would be a drop as in item 4 (6.6). |
| 7. Flush survives a power loss of the Mac | 2026-10-24, measured instead of cutting the power: `FlushFileBuffers` from Windows 0.70 ms median (95th percentile 1.25 ms, 270 calls), about one network round trip; on the Mac, on the `Data_SSD` volume, `fsync` 0.16 ms and `F_FULLFSYNC` 3.07 ms median. The SMB server confirms a flush before the data is on the flash. | **Does not hold.** Risk accepted, no UPS required (decided 2026-10-24): 4.1 and 6.4 state what a power loss of the NAS costs; 6.8, 10.2, GUI spec name it as a possible cause; failure test in 14.3. |
