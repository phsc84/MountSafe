package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"hash/crc32"
	"os"
	"os/signal"
	"sort"
	"time"

	"golang.org/x/sys/windows"
)

// recordSize is the size of one flush record: sequence number, time,
// padding, and a CRC over the rest, so a torn record is recognised.
const recordSize = 4096

func record(seq uint64, now time.Time) []byte {
	b := make([]byte, recordSize)
	binary.BigEndian.PutUint64(b[0:], seq)
	binary.BigEndian.PutUint64(b[8:], uint64(now.UnixNano()))
	for i := 16; i < recordSize-4; i++ {
		b[i] = byte(seq) ^ byte(i)
	}
	binary.BigEndian.PutUint32(b[recordSize-4:], crc32.ChecksumIEEE(b[:recordSize-4]))
	return b
}

func validRecord(b []byte) (uint64, bool) {
	if len(b) != recordSize || crc32.ChecksumIEEE(b[:recordSize-4]) != binary.BigEndian.Uint32(b[recordSize-4:]) {
		return 0, false
	}
	return binary.BigEndian.Uint64(b[0:]), true
}

// runFlushWrite appends a record, calls FlushFileBuffers, and prints the
// record as acknowledged, again and again. Cut the NAS's power while it
// runs; afterwards flush-check must find every acknowledged record (core
// spec 14.5 item 7, 6.4: intent -> data -> commit relies on it).
func runFlushWrite(args []string) error {
	fs := flag.NewFlagSet("flush-write", flag.ContinueOnError)
	file := fs.String("file", "", "file on the share")
	interval := fs.Duration("interval", 100*time.Millisecond, "pause between records")
	if err := parse(fs, args, "file"); err != nil {
		return err
	}
	h, err := openFile(*file, windows.GENERIC_WRITE, windows.FILE_SHARE_READ, windows.CREATE_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h) //nolint:errcheck // the records are flushed one by one
	info("writing records to %s. Cut the NAS's power now (README item 7); Ctrl+C stops.", *file)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	var latencies []time.Duration
	var acked uint64
	defer func() { flushSummary(latencies, acked) }()
	for seq := uint64(1); ; seq++ {
		select {
		case <-stop:
			return nil
		default:
		}
		b := record(seq, time.Now())
		var n uint32
		if err := windows.WriteFile(h, b, &n, nil); err != nil {
			info("%s record %d: write failed: %v", time.Now().Format(time.TimeOnly), seq, err)
			return nil
		}
		start := time.Now()
		if err := windows.FlushFileBuffers(h); err != nil {
			info("%s record %d: FlushFileBuffers failed: %v", time.Now().Format(time.TimeOnly), seq, err)
			return nil
		}
		latencies = append(latencies, time.Since(start))
		acked = seq
		fmt.Printf("\racknowledged %d", seq)
		time.Sleep(*interval)
	}
}

func flushSummary(latencies []time.Duration, acked uint64) {
	fmt.Println()
	info("LAST ACKNOWLEDGED RECORD: %d  <- write this down before the NAS comes back", acked)
	if len(latencies) == 0 {
		return
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	info("FlushFileBuffers: median %s, 95th percentile %s, max %s over %d calls",
		latencies[len(latencies)/2], latencies[len(latencies)*95/100], latencies[len(latencies)-1], len(latencies))
	info("a median below about 1 ms suggests the flush did not wait for the NAS's disk")
}

// runFlushCheck reads the file of flush-write after the NAS is back and
// prints the last complete record (core spec 14.5 item 7).
func runFlushCheck(args []string) error {
	fs := flag.NewFlagSet("flush-check", flag.ContinueOnError)
	file := fs.String("file", "", "file written by flush-write")
	acked := fs.Uint64("acked", 0, "the last acknowledged record flush-write printed")
	if err := parse(fs, args, "file"); err != nil {
		return err
	}
	data, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	var last uint64
	for off := 0; off+recordSize <= len(data); off += recordSize {
		seq, ok := validRecord(data[off : off+recordSize])
		if !ok || seq != last+1 {
			break
		}
		last = seq
	}
	info("%d bytes, last complete record in sequence: %d", len(data), last)
	if *acked > 0 {
		check(last >= *acked, "every acknowledged record survived (acknowledged %d, found %d)", *acked, last)
	}
	return nil
}
