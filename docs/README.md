# Documentation

| Document | Covers | Status |
|---|---|---|
| [SPEC-core.md](SPEC-core.md) | Vault layout and formats, mount and file system operations, configuration, security, test plan | Draft, not implemented |
| [SPEC-gui.md](SPEC-gui.md) | The window: vault list, unlock dialog, Create vault, Mount, Unmount, Add existing vault | Draft, not implemented |
| [SPEC-kdf.md](SPEC-kdf.md) | Argon2id parameters of the vault header: threat model, defaults, limits, memory check; identical copy in RestoreSafe | Draft, not implemented |

The names follow RestoreSafe: `GUIDE-` how to do something, `SPEC-` what the program must do (living, kept up to date with the code), `PLAN-` work in progress.

SPEC-kdf.md is shared with RestoreSafe and kept identical in both repositories: a change to one is made to the other too.

Code comments refer to sections as "core spec 6.1", "GUI spec 5.2" or "KDF spec 4.4".
