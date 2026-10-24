// Package architecture holds no code; its test checks that the imports
// between MountSafe's packages point downward only (core spec 8).
//
// The layers, from top to bottom (a package may import its own layer and
// every layer below it):
//
//	cmd                     mountsafe
//	gui                     the window (GUI spec)
//	mount                   the WinFsp adapter and write-back cache
//	vault                   header, key set, names, file entries, manifests, vault state, commits, lock
//	config, logging         config -> security (Argon2id limits)
//	security                cryptox, recovery, yubikey (copied from RestoreSafe)
//	fsx, problem, buildinfo Windows helpers, the error type, the version
//
// Additional rules:
//
//   - Only internal/mount imports go-winfsp (core spec D12): the vault format
//     knows nothing of WinFsp.
//   - testutil and architecture are test support: exempt from the layers,
//     never imported by production code.
//   - tools holds development tools (the CI runner, the spike programs,
//     kdfbench): outside the layers, separate from the program in both
//     directions.
//
// The test fails for a package that belongs to no layer.
package architecture
