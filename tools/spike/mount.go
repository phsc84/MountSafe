package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/winfsp/go-winfsp"
	"github.com/winfsp/go-winfsp/gofs"
)

// passthrough maps every request to a folder, as go-winfsp's passthrough
// example does. MountSafe will encrypt where this passes through; the
// spike checks only how Windows, Explorer and Office behave on a network
// drive served this way over the NAS (core spec 14.5 item 1, D12, D13).
type passthrough struct {
	dir string
}

func (p *passthrough) path(name string) string { return filepath.Join(p.dir, name) }

func (p *passthrough) OpenFile(name string, flag int, perm os.FileMode) (gofs.File, error) {
	return os.OpenFile(p.path(name), flag, perm)
}

func (p *passthrough) Mkdir(name string, perm os.FileMode) error {
	return os.Mkdir(p.path(name), perm)
}

func (p *passthrough) Stat(name string) (os.FileInfo, error) { return os.Stat(p.path(name)) }

func (p *passthrough) Rename(source, target string) error {
	return os.Rename(p.path(source), p.path(target))
}

func (p *passthrough) Remove(name string) error { return os.Remove(p.path(name)) }

var _ gofs.FileSystem = (*passthrough)(nil)

func runMount(args []string) error {
	fs := flag.NewFlagSet("mount", flag.ContinueOnError)
	src := fs.String("src", "", "folder to serve, e.g. on the NAS share")
	drive := fs.String("drive", "", "drive letter to mount on, e.g. P:")
	prefix := fs.String("prefix", `\mountsafe\spike`, "volume prefix: mounts as a network drive (core spec D13)")
	if err := parse(fs, args, "src", "drive"); err != nil {
		return err
	}
	if st, err := os.Stat(*src); err != nil || !st.IsDir() {
		return fmt.Errorf("-src %s is not a folder: %v", *src, err)
	}
	inner, err := gofs.NewOptions(&passthrough{dir: *src}, gofs.WithCaseInsensitive(true))
	if err != nil {
		return err
	}
	mounted, err := winfsp.Mount(inner, *drive,
		winfsp.VolumePrefix(*prefix),
		winfsp.FileSystemName("MountSafe"),
		winfsp.CaseSensitive(false),
		winfsp.CasePreserveNames(true),
	)
	if err != nil {
		return fmt.Errorf("mount: %w (is WinFsp installed?)", err)
	}
	info("%s mounted on %s as a network drive (prefix %s). Test it as tools/spike/README.md item 1 says.", *src, *drive, *prefix)
	info("Ctrl+C unmounts.")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	mounted.Unmount()
	info("unmounted")
	return nil
}
