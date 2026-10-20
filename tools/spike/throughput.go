package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// alignedBuffer returns n bytes whose start is aligned to 4096 bytes, as
// FILE_FLAG_NO_BUFFERING needs.
func alignedBuffer(n int) []byte {
	const align = 4096
	b := make([]byte, n+align)
	off := align - int(uintptr(unsafe.Pointer(&b[0]))%align)
	return b[off%align : off%align+n]
}

// runThroughput measures sequential and random throughput on the share,
// random with 32, 64 and 256 KiB blocks (core spec 14.5 item 5, D4). The
// random tests bypass the Windows cache (no buffering, write-through), so
// they measure the network and the NAS.
func runThroughput(args []string) error {
	fs := flag.NewFlagSet("throughput", flag.ContinueOnError)
	dir := fs.String("dir", "", "folder on the share to work in")
	size := fs.Int64("size", 1<<30, "size of the test file in bytes")
	duration := fs.Duration("duration", 15*time.Second, "duration of each random test")
	if err := parse(fs, args, "dir"); err != nil {
		return err
	}
	path := filepath.Join(*dir, "spike-throughput.bin")
	defer os.Remove(path)

	const seqBlock = 1 << 20
	*size = *size / seqBlock * seqBlock
	buf := alignedBuffer(seqBlock)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	h, err := openFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, windows.CREATE_ALWAYS,
		windows.FILE_FLAG_NO_BUFFERING|windows.FILE_FLAG_WRITE_THROUGH)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h) //nolint:errcheck // removed afterwards

	start := time.Now()
	for done := int64(0); done < *size; done += seqBlock {
		if err := writeBlock(h, buf, done); err != nil {
			return fmt.Errorf("sequential write: %w", err)
		}
	}
	report("sequential write, 1 MiB blocks", *size, 0, time.Since(start))
	start = time.Now()
	for done := int64(0); done < *size; done += seqBlock {
		if err := readBlock(h, buf, done); err != nil {
			return fmt.Errorf("sequential read: %w", err)
		}
	}
	report("sequential read, 1 MiB blocks", *size, 0, time.Since(start))

	for _, kib := range []int{32, 64, 256} {
		block := kib << 10
		b := buf[:block]
		for _, write := range []bool{false, true} {
			var n int
			start := time.Now()
			for time.Since(start) < *duration {
				off, err := randomOffset(*size, int64(block))
				if err != nil {
					return err
				}
				if write {
					err = writeBlock(h, b, off)
				} else {
					err = readBlock(h, b, off)
				}
				if err != nil {
					return fmt.Errorf("random %d KiB: %w", kib, err)
				}
				n++
			}
			kind := "read"
			if write {
				kind = "write"
			}
			report(fmt.Sprintf("random %s, %d KiB blocks", kind, kib), int64(n*block), n, time.Since(start))
		}
	}
	info("D4 chose 64 KiB chunks; if 32 or 256 KiB are much better per MiB on this NAS, D4 is revisited (core spec 14.5)")
	return nil
}

func randomOffset(size, block int64) (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(size/block))
	if err != nil {
		return 0, err
	}
	return n.Int64() * block, nil
}

func writeBlock(h windows.Handle, b []byte, off int64) error {
	ov := overlappedAt(off)
	var n uint32
	if err := windows.WriteFile(h, b, &n, &ov); err != nil {
		return err
	}
	if int(n) != len(b) {
		return fmt.Errorf("short write: %d of %d", n, len(b))
	}
	return nil
}

func readBlock(h windows.Handle, b []byte, off int64) error {
	ov := overlappedAt(off)
	var n uint32
	if err := windows.ReadFile(h, b, &n, &ov); err != nil {
		return err
	}
	if int(n) != len(b) {
		return fmt.Errorf("short read: %d of %d", n, len(b))
	}
	return nil
}

// overlappedAt gives a synchronous read or write its file offset.
func overlappedAt(off int64) windows.Overlapped {
	return windows.Overlapped{Offset: uint32(off), OffsetHigh: uint32(off >> 32)}
}

func report(what string, bytes int64, ops int, d time.Duration) {
	mib := float64(bytes) / (1 << 20) / d.Seconds()
	if ops > 0 {
		info("%-34s %8.1f MiB/s %8.0f ops/s", what, mib, float64(ops)/d.Seconds())
		return
	}
	info("%-34s %8.1f MiB/s", what, mib)
}
