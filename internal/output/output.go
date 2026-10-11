// Package output preserves the previous local file until a replacement is ready.
package output

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// WriteAtomic renders into a private temporary file before replacing destination.
// Protected inputs cannot be used as outputs, including through hard links.
func WriteAtomic(destination string, write func(io.Writer) error, inputs ...string) error {
	destination, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	if err := checkDestination(destination, inputs); err != nil {
		return err
	}
	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".release-evidence-output-*")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	if err := write(temporary); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("flush temporary output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary output: %w", err)
	}
	// Recheck after rendering, before moving an existing output.
	if err := checkDestination(destination, inputs); err != nil {
		return err
	}
	backupPath := ""
	if _, err := os.Lstat(destination); err == nil {
		backup, err := os.CreateTemp(directory, ".release-evidence-previous-*")
		if err != nil {
			return fmt.Errorf("reserve previous output: %w", err)
		}
		backupPath = backup.Name()
		if err := backup.Close(); err != nil {
			os.Remove(backupPath)
			return fmt.Errorf("close previous-output placeholder: %w", err)
		}
		if err := os.Remove(backupPath); err != nil {
			return fmt.Errorf("prepare previous-output path: %w", err)
		}
		if err := os.Rename(destination, backupPath); err != nil {
			return fmt.Errorf("preserve previous output: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect output path: %w", err)
	}
	if err := os.Rename(temporary.Name(), destination); err != nil {
		if backupPath != "" {
			if restoreErr := os.Rename(backupPath, destination); restoreErr != nil {
				return fmt.Errorf("replace output: %v; previous output retained at %s: %w", err, backupPath, restoreErr)
			}
		}
		return fmt.Errorf("replace output: %w", err)
	}
	if backupPath != "" {
		if err := os.Remove(backupPath); err != nil {
			return fmt.Errorf("output saved, but previous-output cleanup failed: %w", err)
		}
	}
	return nil
}

func checkDestination(destination string, inputs []string) error {
	info, err := os.Lstat(destination)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect output path: %w", err)
	}
	if err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("output must be a regular file, not a directory, link, or special file")
	}
	for _, input := range inputs {
		absolute, err := filepath.Abs(input)
		if err != nil {
			return fmt.Errorf("resolve input path: %w", err)
		}
		if destination == absolute || (runtime.GOOS == "windows" && strings.EqualFold(destination, absolute)) {
			return fmt.Errorf("output must not replace an input packet")
		}
		if info != nil {
			inputInfo, err := os.Stat(input)
			if err != nil {
				return fmt.Errorf("inspect input packet: %w", err)
			}
			if os.SameFile(info, inputInfo) {
				return fmt.Errorf("output must not replace an input packet")
			}
		}
	}
	return nil
}
