package main

import (
	"flag"
	"strings"

	"golang.org/x/sys/windows"
)

// runVolInfo shows what Windows reports about the mounted drive, as
// fsutil fsinfo volumeinfo does, without administrator rights (core spec
// 14.5 item 1, 6.2 step 8: case-insensitive, case-preserving).
func runVolInfo(args []string) error {
	fs := flag.NewFlagSet("volinfo", flag.ContinueOnError)
	drive := fs.String("drive", "", "the mounted drive, e.g. P:")
	if err := parse(fs, args, "drive"); err != nil {
		return err
	}
	root, err := windows.UTF16PtrFromString(strings.TrimSuffix(*drive, `\`) + `\`)
	if err != nil {
		return err
	}
	var label, fsName [windows.MAX_PATH + 1]uint16
	var serial, maxComponent, flags uint32
	if err := windows.GetVolumeInformation(root, &label[0], uint32(len(label)), &serial, &maxComponent, &flags, &fsName[0], uint32(len(fsName))); err != nil {
		return err
	}
	name := windows.UTF16ToString(fsName[:])
	check(name == "MountSafe", "file system name: %q", name)
	check(flags&windows.FILE_CASE_PRESERVED_NAMES != 0, "case-preserving names")
	check(flags&windows.FILE_CASE_SENSITIVE_SEARCH == 0, "no case-sensitive search (FILE_CASE_SENSITIVE_SEARCH not set)")
	info("volume label %q, maximum name length %d, flags 0x%08x", windows.UTF16ToString(label[:]), maxComponent, flags)
	kind := windows.GetDriveType(root)
	check(kind == windows.DRIVE_REMOTE, "drive type: network drive (%d; 4 is DRIVE_REMOTE, 3 a local disk)", kind)
	return nil
}
