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
func runLockHold(args []string) error {
	fs := flag.NewFlagSet("lock-hold", flag.ContinueOnError)
	file := fs.String("file", "", "lock file on the share")
	heartbeat := fs.Duration("heartbeat", 5*time.Second, "interval of the heartbeat write")
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
	lost := time.Time{}
	for {
		select {
		case <-stop:
			windows.CloseHandle(h) //nolint:errcheck // released at exit anyway
			info("released")
			return nil
		case now := <-tick.C:
			line := fmt.Sprintf("held by %s, PID %d, heartbeat %s\r\n", host, os.Getpid(), now.Format(time.RFC3339))
			err := writeAt(h, []byte(line))
			switch {
			case err != nil && lost.IsZero():
				lost = now
				info("%s heartbeat failed: %v; trying to re-open (core spec 6.6)", now.Format(time.TimeOnly), err)
			case err != nil:
				windows.CloseHandle(h) //nolint:errcheck // the handle is broken already
				if nh, oerr := openLock(*file); oerr == nil {
					h = nh
					info("%s re-opened after %s", now.Format(time.TimeOnly), now.Sub(lost).Round(time.Second))
					lost = time.Time{}
				} else if errors.Is(oerr, windows.ERROR_SHARING_VIOLATION) {
					info("%s re-open refused: someone else holds the lock now (core spec 6.6: read-only)", now.Format(time.TimeOnly))
				}
			case !lost.IsZero():
				info("%s heartbeat works again after %s, without re-opening", now.Format(time.TimeOnly), now.Sub(lost).Round(time.Second))
				lost = time.Time{}
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
	for {
		h, err := openLock(*file)
		if err == nil {
			windows.CloseHandle(h) //nolint:errcheck // only probing
			if *wait && time.Since(start) > time.Second {
				check(true, "the lock became free after %s", time.Since(start).Round(time.Second))
			} else {
				info("the lock is free: nobody holds it")
			}
			return nil
		}
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			check(false, "open for writing failed, but not with a sharing violation: %v", err)
			return nil
		}
		if !*wait || time.Since(start) < time.Second {
			check(true, "a second open for writing is refused with a sharing violation")
			holder, rerr := os.ReadFile(*file)
			check(rerr == nil, "the holder can still be read: %q %v", holder, rerr)
			if !*wait {
				return nil
			}
			info("waiting until the lock is free (kill the holder, or cut its network)...")
		}
		time.Sleep(time.Second)
	}
}
