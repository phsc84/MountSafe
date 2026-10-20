package main

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileBasicInfo is FILE_BASIC_INFO of GetFileInformationByHandleEx: the four
// times as FILETIME values (100 ns since 1601) and the attributes.
type fileBasicInfo struct {
	CreationTime   int64
	LastAccessTime int64
	LastWriteTime  int64
	ChangeTime     int64
	FileAttributes uint32
	_              uint32
}

// openFile opens path with CreateFile; a folder needs
// FILE_FLAG_BACKUP_SEMANTICS in flags.
func openFile(path string, access, share, disposition, flags uint32) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(p, access, share, nil, disposition, flags, 0)
}

// openForTimes opens a file or folder to read and set its times.
func openForTimes(path string) (windows.Handle, error) {
	return openFile(path, windows.FILE_READ_ATTRIBUTES|windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS)
}

// basicInfo reads the times and attributes of an open handle.
func basicInfo(h windows.Handle) (fileBasicInfo, error) {
	var bi fileBasicInfo
	err := windows.GetFileInformationByHandleEx(h, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&bi)), uint32(unsafe.Sizeof(bi)))
	return bi, err
}

// basicInfoOf reads the times and attributes of path.
func basicInfoOf(path string) (fileBasicInfo, error) {
	h, err := openForTimes(path)
	if err != nil {
		return fileBasicInfo{}, err
	}
	defer windows.CloseHandle(h) //nolint:errcheck // read-only use
	return basicInfo(h)
}

// setTimes sets creation, last-access and last-write time of path to t.
func setTimes(path string, t time.Time) error {
	h, err := openForTimes(path)
	if err != nil {
		return err
	}
	ft := windows.NsecToFiletime(t.UnixNano())
	if err := windows.SetFileTime(h, &ft, &ft, &ft); err != nil {
		windows.CloseHandle(h) //nolint:errcheck // the SetFileTime error is the one to report
		return err
	}
	return windows.CloseHandle(h)
}

// fileTime converts a FILETIME value of fileBasicInfo to a time.
func fileTime(v int64) time.Time {
	ft := windows.Filetime{LowDateTime: uint32(v), HighDateTime: uint32(v >> 32)}
	return time.Unix(0, ft.Nanoseconds()).UTC()
}

// moveNoReplace renames from to to with MoveFileEx without
// MOVEFILE_REPLACE_EXISTING: it fails when to exists. os.Rename replaces.
func moveNoReplace(from, to string) error {
	f, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(f, t, 0)
}
