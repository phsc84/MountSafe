// Command kdfbench measures the unlock time of Argon2id with the parameters
// of KDF spec section 6, on the machine it runs on, the way RestoreSafe and
// MountSafe call it (golang.org/x/crypto/argon2.IDKey, 32-byte key).
//
//	go run ./tools/kdfbench                      # the configurations of KDF spec 6
//	go run ./tools/kdfbench -m 6144 -t 1 -p 4    # one configuration, m in MiB
//
// Before each configuration it checks the free physical memory and skips one
// that does not fit with 1 GiB to spare, unless -force is given (KDF spec 4.4:
// above the free memory, Windows pages or the process ends).
//
// It is a development tool: it imports nothing of MountSafe (SPEC-ci 3.2).
package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"time"
	"unsafe"

	"golang.org/x/crypto/argon2"
	"golang.org/x/sys/windows"
)

// config is one parameter set: memory in MiB, passes, lanes.
type config struct {
	name    string
	m, t, p uint32
}

// spec6 are the rows of KDF spec section 6 for the development PC.
var spec6 = []config{
	{"old default", 512, 3, 4},
	{"proposed, t=1", 4096, 1, 4},
	{"proposed, t=2", 4096, 2, 4},
	{"8 GiB, t=1", 8192, 1, 4},
}

func main() {
	m := flag.Uint("m", 0, "memory in MiB (with -t and -p: measure only this configuration)")
	t := flag.Uint("t", 1, "passes")
	p := flag.Uint("p", 4, "lanes (threads)")
	runs := flag.Int("runs", 3, "derivations per configuration; the median counts")
	force := flag.Bool("force", false, "run configurations that do not fit into the free memory (pages, or ends the process)")
	flag.Parse()

	configs := spec6
	if *m > 0 {
		configs = []config{{"custom", uint32(*m), uint32(*t), uint32(*p)}}
	}
	total, _, err := memory()
	if err != nil {
		fmt.Fprintln(os.Stderr, "memory:", err)
		os.Exit(1)
	}
	fmt.Printf("machine: %d logical CPUs, %.1f GiB physical memory, %s\n\n", runtime.NumCPU(), gib(total), runtime.Version())
	fmt.Printf("%-15s %8s %2s %2s  %10s %10s %10s  %s\n", "", "m (MiB)", "t", "p", "median", "min", "max", "free memory before")

	secret := []byte("correct horse battery staple")
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, c := range configs {
		var times []time.Duration
		var freeBefore uint64
		skipped := ""
		for i := 0; i < *runs; i++ {
			release()
			_, free, err := memory()
			if err != nil {
				fmt.Fprintln(os.Stderr, "memory:", err)
				os.Exit(1)
			}
			if i == 0 {
				freeBefore = free
			}
			need := uint64(c.m)<<20 + 1<<30
			if free < need && !*force {
				skipped = fmt.Sprintf("skipped: needs %.1f GiB free (m + 1 GiB), %.1f GiB are; -force runs it anyway", gib(need), gib(free))
				break
			}
			start := time.Now()
			key := argon2.IDKey(secret, salt, c.t, c.m<<10, uint8(c.p), 32)
			times = append(times, time.Since(start))
			_ = key
		}
		if skipped != "" {
			fmt.Printf("%-15s %8d %2d %2d  %s\n", c.name, c.m, c.t, c.p, skipped)
			continue
		}
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		fmt.Printf("%-15s %8d %2d %2d  %10s %10s %10s  %.1f GiB\n", c.name, c.m, c.t, c.p,
			round(times[len(times)/2]), round(times[0]), round(times[len(times)-1]), gib(freeBefore))
	}
	fmt.Println("\nCopy the table into KDF spec section 6. Close other programs first; the median counts.")
}

// release returns the memory of the previous derivation to Windows, so the
// next one starts from the same free memory.
func release() {
	runtime.GC()
	debug.FreeOSMemory()
}

// memoryStatusEx is MEMORYSTATUSEX of GlobalMemoryStatusEx.
type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

var globalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

// memory returns the total and the available physical memory in bytes (KDF
// spec 4.4 step 1).
func memory() (total, avail uint64, err error) {
	ms := memoryStatusEx{length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if r, _, e := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms))); r == 0 {
		return 0, 0, e
	}
	return ms.totalPhys, ms.availPhys, nil
}

func gib(b uint64) float64 { return float64(b) / (1 << 30) }

func round(d time.Duration) time.Duration { return d.Round(10 * time.Millisecond) }
