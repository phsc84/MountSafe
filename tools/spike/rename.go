package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// runRename checks on one PC that a rename on the share replaces nothing,
// keeps the content, works for files and folders, and handles a case-only
// change (core spec 14.5 item 3).
func runRename(args []string) error {
	fs := flag.NewFlagSet("rename", flag.ContinueOnError)
	dir := fs.String("dir", "", "folder on the share to work in")
	if err := parse(fs, args, "dir"); err != nil {
		return err
	}
	work, err := os.MkdirTemp(*dir, "spike-rename-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	a, b := filepath.Join(work, "A.f"), filepath.Join(work, "B.f")
	if err := os.WriteFile(a, []byte("content of A"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(b, []byte("content of B"), 0o600); err != nil {
		return err
	}
	err = moveNoReplace(a, b)
	check(errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS),
		"rename onto an existing file is refused: %v", err)
	got, _ := os.ReadFile(b)
	check(string(got) == "content of B", "the existing file is unchanged: %q", got)

	c := filepath.Join(work, "C.f")
	check(moveNoReplace(a, c) == nil, "rename to a free name works")
	got, _ = os.ReadFile(c)
	check(string(got) == "content of A", "the renamed file keeps its content: %q", got)
	_, err = os.Stat(a)
	check(errors.Is(err, os.ErrNotExist), "the old name is gone: %v", err)

	lower := filepath.Join(work, "c.f")
	check(moveNoReplace(c, lower) == nil, "case-only rename works")
	names, _ := os.ReadDir(work)
	found := ""
	for _, n := range names {
		if n.Name() == "c.f" || n.Name() == "C.f" {
			found = n.Name()
		}
	}
	check(found == "c.f", "the listing shows the new case: %q", found)

	d1, d2 := filepath.Join(work, "D1.d"), filepath.Join(work, "D2.d")
	if err := os.Mkdir(d1, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(d1, "inner.f"), []byte("inner"), 0o600); err != nil {
		return err
	}
	check(moveNoReplace(d1, d2) == nil, "rename of a non-empty folder works")
	got, _ = os.ReadFile(filepath.Join(d2, "inner.f"))
	check(string(got) == "inner", "the folder's content moved with it: %q", got)
	return nil
}

// flipContent is the content of the file that rename-flip renames back and
// forth: large enough that a copy instead of a rename would be seen half
// written.
var flipContent = bytes.Repeat([]byte("0123456789abcdef"), 64*1024) // 1 MiB

// runRenameFlip renames a file between two names as fast as it can, for
// rename-watch on a second PC (core spec 14.5 item 3).
func runRenameFlip(args []string) error {
	fs := flag.NewFlagSet("rename-flip", flag.ContinueOnError)
	dir := fs.String("dir", "", "folder on the share, the same for rename-watch")
	count := fs.Int("count", 2000, "number of renames")
	if err := parse(fs, args, "dir"); err != nil {
		return err
	}
	a, b := filepath.Join(*dir, "flip-A.f"), filepath.Join(*dir, "flip-B.f")
	os.Remove(b)
	if err := os.WriteFile(a, flipContent, 0o600); err != nil {
		return err
	}
	info("renaming %s <-> %s %d times; start rename-watch on the other PC now", filepath.Base(a), filepath.Base(b), *count)
	time.Sleep(5 * time.Second)
	start := time.Now()
	from, to := a, b
	retries := 0
	for i := 0; i < *count; i++ {
		err := moveNoReplace(from, to)
		// The watcher's read handle can still be open at the server for a
		// moment; a vault has one mount only (D8), so this is the test's.
		for try := 0; errors.Is(err, windows.ERROR_SHARING_VIOLATION) && try < 1000; try++ {
			retries++
			time.Sleep(time.Millisecond)
			err = moveNoReplace(from, to)
		}
		if err != nil {
			check(false, "rename %d: %v", i, err)
			return nil
		}
		from, to = to, from
	}
	elapsed := time.Since(start)
	check(true, "%d renames without an error", *count)
	info("%.1f ms per rename; %d retries while the watcher had the file open", float64(elapsed.Microseconds())/1000/float64(*count), retries)
	return os.Remove(from)
}

// readShared reads a whole file, letting others rename or delete it while
// it is open (FILE_SHARE_DELETE, which os.ReadFile does not give).
func readShared(path string) ([]byte, error) {
	h, err := openFile(path, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	return io.ReadAll(f)
}

// runRenameWatch reads the file of rename-flip under either name, again
// and again, and checks that whatever it reads is the whole content: a
// rename that is a copy and a delete underneath would be seen half written
// or twice (core spec 14.5 item 3).
func runRenameWatch(args []string) error {
	fs := flag.NewFlagSet("rename-watch", flag.ContinueOnError)
	dir := fs.String("dir", "", "folder on the share, the same for rename-flip")
	duration := fs.Duration("duration", 60*time.Second, "how long to watch")
	if err := parse(fs, args, "dir"); err != nil {
		return err
	}
	a, b := filepath.Join(*dir, "flip-A.f"), filepath.Join(*dir, "flip-B.f")
	var reads, partial, missing, both int
	deadline := time.Now().Add(*duration)
	for time.Now().Before(deadline) {
		ga, ea := readShared(a)
		gb, eb := readShared(b)
		for _, r := range []struct {
			got []byte
			err error
		}{{ga, ea}, {gb, eb}} {
			if r.err == nil {
				reads++
				if !bytes.Equal(r.got, flipContent) {
					partial++
				}
			}
		}
		switch {
		case ea != nil && eb != nil:
			missing++
		case ea == nil && eb == nil:
			both++
		}
	}
	check(reads > 0, "read the file %d times", reads)
	check(partial == 0, "never read a partial or wrong content (%d times)", partial)
	info("neither name found in %d rounds, both in %d: expected now and then, the two reads of a round are not one snapshot", missing, both)
	if reads == 0 {
		return fmt.Errorf("nothing read: is rename-flip running on the other PC, on the same folder?")
	}
	return nil
}
