//go:build !windows

package repository

import (
	"io/fs"
	"os"
)

func isLinkEntry(entry fs.DirEntry) (bool, error) {
	return entry.Type()&os.ModeSymlink != 0, nil
}

func isLinkInfo(info fs.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0
}
