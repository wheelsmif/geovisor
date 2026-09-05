package output

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFilesReplacesExistingFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(path, []byte("old"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := WriteFiles([]File{{Path: path, Data: []byte("new")}}); err != nil {
		t.Fatalf("write files: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("data = %q, want new", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("permissions = %o, want no group/other access", info.Mode().Perm())
	}
}

func TestWriteFilesStageFailurePreservesExistingOutputs(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	existing := filepath.Join(directory, "artifact.json")
	if err := os.WriteFile(existing, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	notDirectory := filepath.Join(directory, "not-a-directory")
	if err := os.WriteFile(notDirectory, []byte("blocker"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := WriteFiles([]File{
		{Path: existing, Data: []byte("replacement")},
		{Path: filepath.Join(notDirectory, "second.json"), Data: []byte("second")},
	})
	if err == nil {
		t.Fatal("WriteFiles unexpectedly succeeded")
	}
	data, readErr := os.ReadFile(existing)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "original" {
		t.Fatalf("existing output = %q, want original", data)
	}
}

func TestWriteFilesRejectsCollisionsBeforeWriting(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "artifact.json")
	err := WriteFiles([]File{
		{Path: path, Data: []byte("first")},
		{Path: path, Data: []byte("second")},
	})
	if err == nil {
		t.Fatal("duplicate output unexpectedly succeeded")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("duplicate output created a file: %v", statErr)
	}
}
