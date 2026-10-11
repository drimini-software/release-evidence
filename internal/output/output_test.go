package output

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestFailedRenderPreservesPreviousFile(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "report.html")
	if err := os.WriteFile(destination, []byte("previous report"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := WriteAtomic(destination, func(writer io.Writer) error {
		io.WriteString(writer, "partial report")
		return errors.New("render failed")
	})
	if err == nil {
		t.Fatal("failed render succeeded")
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "previous report" {
		t.Fatalf("previous report changed: %s, %v", content, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files remain: %v, %v", entries, err)
	}
}

func TestWriteProtectsInputPacketsAndNonRegularDestinations(t *testing.T) {
	directory := t.TempDir()
	input := filepath.Join(directory, "packet.json")
	if err := os.WriteFile(input, []byte("input packet"), 0o600); err != nil {
		t.Fatal(err)
	}
	write := func(writer io.Writer) error { _, err := io.WriteString(writer, "report"); return err }
	if err := WriteAtomic(input, write, input); err == nil {
		t.Fatal("replaced input packet")
	}
	if err := WriteAtomic(directory, write); err == nil {
		t.Fatal("moved output directory")
	}
	hardlink := filepath.Join(directory, "alias.json")
	if err := os.Link(input, hardlink); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if err := WriteAtomic(hardlink, write, input); err == nil {
		t.Fatal("replaced hard-linked input packet")
	}
	content, err := os.ReadFile(input)
	if err != nil || string(content) != "input packet" {
		t.Fatalf("input changed: %s, %v", content, err)
	}
}

func TestWriteRejectsSymbolicLink(t *testing.T) {
	directory := t.TempDir()
	target, link := filepath.Join(directory, "target"), filepath.Join(directory, "link")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if err := WriteAtomic(link, func(writer io.Writer) error { _, err := io.WriteString(writer, "report"); return err }); err == nil {
		t.Fatal("followed output symbolic link")
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "target" {
		t.Fatalf("target changed: %s, %v", content, err)
	}
}

func TestSuccessfulWriteReplacesOnlyReport(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "report.html")
	for _, value := range []string{"first", "second"} {
		if err := WriteAtomic(destination, func(writer io.Writer) error { _, err := io.WriteString(writer, value); return err }); err != nil {
			t.Fatal(err)
		}
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "second" {
		t.Fatalf("replacement failed: %s, %v", content, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("unexpected files: %v, %v", entries, err)
	}
}
