//go:build windows

package repository

import (
	"io/fs"
	"os"
	"syscall"
)

func isLinkEntry(entry fs.DirEntry) (bool, error) {
	if entry.Type()&os.ModeSymlink != 0 {
		return true, nil
	}
	info, err := entry.Info()
	if err != nil {
		return false, err
	}
	return isLinkInfo(info), nil
}

func isLinkInfo(info fs.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
