package main

import (
	"flag"
	"os"
	"path/filepath"
	"time"
)

// vaultTime stands for the vault time of core spec 5.6: whole seconds, far
// from now, so a time the server sets by itself stands out.
var vaultTime = time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)

// runTimes checks that creation, last-write and last-access time of a file
// and a folder on the share can be set, and stay set after a rename and
// after the next SetFileTime following a write (core spec 14.5 item 2, 5.6).
// It also shows the change time, which a client cannot set.
func runTimes(args []string) error {
	fs := flag.NewFlagSet("times", flag.ContinueOnError)
	dir := fs.String("dir", "", "folder on the share to work in")
	keep := fs.Bool("keep", false, "keep the work folder")
	if err := parse(fs, args, "dir"); err != nil {
		return err
	}
	work, err := os.MkdirTemp(*dir, "spike-times-")
	if err != nil {
		return err
	}
	if !*keep {
		defer os.RemoveAll(work)
	}
	file := filepath.Join(work, "file.f")
	folder := filepath.Join(work, "folder.d")
	if err := os.WriteFile(file, []byte("content"), 0o600); err != nil {
		return err
	}
	if err := os.Mkdir(folder, 0o700); err != nil {
		return err
	}

	for _, p := range []string{file, folder} {
		if err := setTimes(p, vaultTime); err != nil {
			check(false, "SetFileTime on %s: %v", filepath.Base(p), err)
			continue
		}
		expectVaultTime(p, "after SetFileTime")
	}

	// A rename must not change the times (D10: a time change is a rename).
	file2, folder2 := filepath.Join(work, "file2.f"), filepath.Join(work, "folder2.d")
	if err := moveNoReplace(file, file2); err != nil {
		return err
	}
	if err := moveNoReplace(folder, folder2); err != nil {
		return err
	}
	expectVaultTime(file2, "after a rename")
	expectVaultTime(folder2, "after a rename")

	// A write changes last-write; MountSafe sets the vault time again after
	// every change, and it must stick, also some seconds later.
	f, err := os.OpenFile(file2, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(" more"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	showTimes(file2, "after a write, before SetFileTime")
	if err := os.WriteFile(filepath.Join(folder2, "inner.f"), []byte("x"), 0o600); err != nil {
		return err
	}
	showTimes(folder2, "after creating an entry in it, before SetFileTime")
	for _, p := range []string{file2, folder2} {
		if err := setTimes(p, vaultTime); err != nil {
			check(false, "SetFileTime on %s after a change: %v", filepath.Base(p), err)
			continue
		}
		expectVaultTime(p, "after a change and SetFileTime")
	}
	time.Sleep(5 * time.Second)
	expectVaultTime(file2, "5 s later")
	expectVaultTime(folder2, "5 s later")
	showTimes(file2, "final (ChangeTime is the server's; core spec 5.6 expects it to be the real time)")
	return nil
}

// expectVaultTime checks that creation, last-write and last-access time of
// path are the vault time.
func expectVaultTime(path, when string) {
	bi, err := basicInfoOf(path)
	if err != nil {
		check(false, "%s %s: %v", filepath.Base(path), when, err)
		return
	}
	c, w, a := fileTime(bi.CreationTime), fileTime(bi.LastWriteTime), fileTime(bi.LastAccessTime)
	check(c.Equal(vaultTime) && w.Equal(vaultTime) && a.Equal(vaultTime),
		"%s %s: creation %s, last-write %s, last-access %s (want %s)",
		filepath.Base(path), when, stamp(c), stamp(w), stamp(a), stamp(vaultTime))
}

// showTimes prints all four times of path.
func showTimes(path, when string) {
	bi, err := basicInfoOf(path)
	if err != nil {
		info("%s %s: %v", filepath.Base(path), when, err)
		return
	}
	info("%s %s: creation %s, last-write %s, last-access %s, change %s", filepath.Base(path), when,
		stamp(fileTime(bi.CreationTime)), stamp(fileTime(bi.LastWriteTime)),
		stamp(fileTime(bi.LastAccessTime)), stamp(fileTime(bi.ChangeTime)))
}

func stamp(t time.Time) string { return t.Format(time.RFC3339Nano) }
