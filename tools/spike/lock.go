package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"golang.org/x/sys/windows"
)

// openLock opens path as core spec 6.2 step 1 does: read and write, other
// handles may only read.
func openLock(path string) (windows.Handle, error) {
	return openFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ,
		windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL)
}

// writeAt writes b at offset 0 of h and truncates there.
func writeAt(h windows.Handle, b []byte) error {
	if _, err := windows.Seek(h, 0, 0); err != nil {
		return err
	}
	var n uint32
	if err := windows.WriteFile(h, b, &n, nil); err != nil {
		return err
	}
	return windows.SetEndOfFile(h)
}

// runLockHold holds the lock like a mount does and writes a heartbeat into
// it, so a lost handle shows (core spec 14.5 items 4 and 6, D8, 6.6).
//
// After a failed heartbeat it first keeps the handle and retries on it for
// -keep: Windows can reattach an open handle to the server after a
// reconnect (SMB durable handles), but only while the handle stays open.
// Then it closes the handle and re-opens the file, and reports how long the
// server refused that because it still held the old open.
func runLockHold(args []string) error {
	fs := flag.NewFlagSet("lock-hold", flag.ContinueOnError)
	file := fs.String("file", "", "lock file on the share")
	heartbeat := fs.Duration("heartbeat", 5*time.Second, "interval of the heartbeat write")
	keep := fs.Duration("keep", 3*time.Minute, "how long to retry on the original handle before re-opening")
	if err := parse(fs, args, "file"); err != nil {
		return err
	}
	h, err := openLock(*file)
	if err != nil {
		return fmt.Errorf("open %s: %w", *file, err)
	}
	host, _ := os.Hostname()
	info("holding %s as %s (PID %d). Ctrl+C releases it; killing this process tests the release.", *file, host, os.Getpid())
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	tick := time.NewTicker(*heartbeat)
	defer tick.Stop()
	var lost, refused time.Time // when the heartbeat failed; when a re-open was first refused
	closed := false             // the original handle was given up
	var lastErr string
	for {
		select {
		case <-stop:
			if !closed {
				windows.CloseHandle(h) //nolint:errcheck // released at exit anyway
			}
			info("released")
			return nil
		case now := <-tick.C:
			at := now.Format(time.TimeOnly)
			if !closed {
				line := fmt.Sprintf("held by %s, PID %d, heartbeat %s\r\n", host, os.Getpid(), now.Format(time.RFC3339))
				err := writeAt(h, []byte(line))
				switch {
				case err == nil && !lost.IsZero():
					check(true, "%s heartbeat works again on the ORIGINAL handle after %s (durable handle reconnected)", at, now.Sub(lost).Round(time.Second))
					lost, lastErr = time.Time{}, ""
				case err != nil && lost.IsZero():
					lost, lastErr = now, err.Error()
					info("%s heartbeat failed: %v; retrying on the original handle for %s", at, err, *keep)
				case err != nil && now.Sub(lost) >= *keep:
					windows.CloseHandle(h) //nolint:errcheck // the handle is broken
					closed = true
					info("%s original handle given up after %s; re-opening (core spec 6.6)", at, now.Sub(lost).Round(time.Second))
				case err != nil && err.Error() != lastErr:
					lastErr = err.Error()
					info("%s still failing on the original handle: %v", at, err)
				}
				continue
			}
			nh, oerr := openLock(*file)
			switch {
			case oerr == nil:
				h, closed = nh, false
				detail := ""
				if !refused.IsZero() {
					detail = fmt.Sprintf("; the server held the old open for %s after the first refusal", now.Sub(refused).Round(time.Second))
				}
				check(true, "%s re-opened %s after the heartbeat failed%s", at, now.Sub(lost).Round(time.Second), detail)
				lost, refused, lastErr = time.Time{}, time.Time{}, ""
			case errors.Is(oerr, windows.ERROR_SHARING_VIOLATION) && refused.IsZero():
				refused = now
				holder, _ := readShared(*file)
				info("%s re-open refused (sharing violation); the file says %q. Keep it running until it re-opens.", at, holder)
			case !errors.Is(oerr, windows.ERROR_SHARING_VIOLATION) && oerr.Error() != lastErr:
				lastErr = oerr.Error()
				info("%s re-open failed: %v", at, oerr)
			}
		}
	}
}

// runLockTry tries to open the lock as a second mount would, and reads who
// holds it (core spec 14.5 item 4, 6.2 step 1). With -wait, it tries every
// second until the lock is free and says how long that took.
func runLockTry(args []string) error {
	fs := flag.NewFlagSet("lock-try", flag.ContinueOnError)
	file := fs.String("file", "", "lock file on the share")
	wait := fs.Bool("wait", false, "try every second until the lock is free")
	if err := parse(fs, args, "file"); err != nil {
		return err
	}
	start := time.Now()
	var refused time.Time // the first sharing violation
	lastErr := ""
	for {
		now := time.Now()
		at := now.Format(time.TimeOnly)
		h, err := openLock(*file)
		switch {
		case err == nil:
			windows.CloseHandle(h) //nolint:errcheck // only probing
			switch {
			case refused.IsZero() && !*wait:
				info("the lock is free: nobody holds it")
			case refused.IsZero():
				info("%s the lock is free: nobody holds it (the share answered after %s)", at, now.Sub(start).Round(time.Second))
			default:
				check(true, "%s the lock became free %s after the first refusal", at, now.Sub(refused).Round(time.Second))
			}
			return nil
		case errors.Is(err, windows.ERROR_SHARING_VIOLATION):
			if refused.IsZero() {
				refused = now
				check(true, "%s a second open for writing is refused with a sharing violation", at)
				holder, rerr := readShared(*file)
				check(rerr == nil, "the holder can still be read: %q %v", holder, rerr)
				if !*wait {
					return nil
				}
				info("waiting until the lock is free (kill the holder, or cut its network)...")
			}
		case !*wait:
			check(false, "open for writing failed, but not with a sharing violation: %v", err)
			return nil
		case err.Error() != lastErr:
			// -wait also waits through a network that is still down.
			lastErr = err.Error()
			info("%s not reachable yet: %v", at, err)
		}
		time.Sleep(time.Second)
	}
}
