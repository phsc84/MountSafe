// Command spike checks the assumptions of core spec 14.5 against the target
// NAS before implementation starts. Each command checks one point; the
// README says how to run each and what to record.
//
//	go run ./tools/spike mount -src \\nas\share\spike -drive P:
//	go run ./tools/spike times -dir \\nas\share\spike
//	go run ./tools/spike rename -dir \\nas\share\spike
//	go run ./tools/spike rename-flip -dir \\nas\share\spike      (PC 1)
//	go run ./tools/spike rename-watch -dir \\nas\share\spike     (PC 2)
//	go run ./tools/spike lock-hold -file \\nas\share\spike\vault.lock
//	go run ./tools/spike lock-try -file \\nas\share\spike\vault.lock [-wait]
//	go run ./tools/spike throughput -dir \\nas\share\spike
//	go run ./tools/spike flush-write -file \\nas\share\spike\flush.bin
//	go run ./tools/spike flush-check -file \\nas\share\spike\flush.bin -acked <n>
//
// It is a development tool: it imports nothing of MountSafe (SPEC-ci 3.2).
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

// command is one check of the spike.
type command struct {
	item  string // the item of core spec 14.5
	usage string
	run   func(args []string) error
}

var commands = map[string]command{
	"mount":        {"1", "-src <dir> -drive <X:> [-prefix \\mountsafe\\spike]", runMount},
	"times":        {"2", "-dir <dir>", runTimes},
	"rename":       {"3", "-dir <dir>", runRename},
	"rename-flip":  {"3", "-dir <dir> [-count n]", runRenameFlip},
	"rename-watch": {"3", "-dir <dir> [-duration d]", runRenameWatch},
	"lock-hold":    {"4, 6", "-file <path> [-heartbeat d]", runLockHold},
	"lock-try":     {"4", "-file <path> [-wait]", runLockTry},
	"throughput":   {"5", "-dir <dir> [-size bytes] [-duration d]", runThroughput},
	"flush-write":  {"7", "-file <path> [-interval d]", runFlushWrite},
	"flush-check":  {"7", "-file <path> [-acked n]", runFlushCheck},
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, ok := commands[os.Args[1]]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err := cmd.run(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if failures > 0 {
		fmt.Printf("\n%d check(s) FAILED\n", failures)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: go run ./tools/spike <command> [flags]   (tools/spike/README.md)")
	fmt.Fprintln(os.Stderr)
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c := commands[name]
		fmt.Fprintf(os.Stderr, "  %-13s %-50s core spec 14.5 item %s\n", name, c.usage, c.item)
	}
}

// failures counts the failed checks of the run; any makes the exit code 1.
var failures int

// check prints the outcome of one check as a line to copy into the report.
func check(ok bool, format string, args ...any) {
	status := "PASS"
	if !ok {
		status = "FAIL"
		failures++
	}
	fmt.Printf("%s  %s\n", status, fmt.Sprintf(format, args...))
}

// info prints a measured value that has no pass or fail.
func info(format string, args ...any) {
	fmt.Printf("INFO  %s\n", fmt.Sprintf(format, args...))
}

// parse parses the flags of a command and checks that the required flags
// (by name) are set.
func parse(fs *flag.FlagSet, args []string, required ...string) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	var missing []string
	for _, name := range required {
		if fs.Lookup(name).Value.String() == "" {
			missing = append(missing, "-"+name)
		}
	}
	if len(missing) > 0 {
		fs.Usage()
		return fmt.Errorf("%s: missing %s", fs.Name(), strings.Join(missing, ", "))
	}
	return nil
}
