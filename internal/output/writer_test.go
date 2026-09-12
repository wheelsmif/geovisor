package output

import (
	"context"
	"errors"
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
	if err := WriteFiles(context.Background(), []File{{Path: path, Data: []byte("new")}}); err != nil {
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

	err := WriteFiles(context.Background(), []File{
		{Path: existing, Data: []byte("replacement")},
		{Path: filepath.Join(notDirectory, "second.json"), Data: []byte("second")},
	})
	if err == nil {
		t.Fatal("WriteFiles unexpectedly succeeded")
	}
	assertFileData(t, existing, "original")
}

func TestWriteFilesRejectsCollisionsBeforeWriting(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "artifact.json")
	err := WriteFiles(context.Background(), []File{
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

func TestWriteFilesRejectsEmptyPath(t *testing.T) {
	t.Parallel()
	err := WriteFiles(context.Background(), []File{{Path: "  ", Data: []byte("x")}})
	if err == nil {
		t.Fatal("empty path unexpectedly succeeded")
	}
}

func TestWriteFilesRejectsNilContext(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "artifact.json")
	//lint:ignore SA1012 this test asserts WriteFiles rejects a nil context
	err := WriteFiles(nil, []File{{Path: path, Data: []byte("new")}})
	if err == nil {
		t.Fatal("nil context unexpectedly succeeded")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("nil-context write created a file: %v", statErr)
	}
}

func TestWriteFilesHonorsCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "artifact.json")
	err := WriteFiles(ctx, []File{{Path: path, Data: []byte("new")}})
	if err == nil {
		t.Fatal("canceled context unexpectedly succeeded")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("canceled write created a file: %v", statErr)
	}
}

func TestWriteFilesMkdirAllFailure(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	existing := filepath.Join(directory, "kept.json")
	if err := os.WriteFile(existing, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	ops := productionOps()
	ops.mkdirAll = func(string, os.FileMode) error {
		return errors.New("injected mkdir failure")
	}
	err := writeFiles(context.Background(), []File{
		{Path: existing, Data: []byte("replacement")},
	}, ops)
	if err == nil {
		t.Fatal("mkdir failure unexpectedly succeeded")
	}
	assertFileData(t, existing, "original")
}

func TestWriteFilesReplaceFailureRollsBackEarlierFiles(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	first := filepath.Join(directory, "first.json")
	second := filepath.Join(directory, "second.json")
	third := filepath.Join(directory, "third.json")
	if err := os.WriteFile(first, []byte("old-first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("old-second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(third, []byte("old-third"), 0o600); err != nil {
		t.Fatal(err)
	}

	ops := productionOps()
	ops.replace = func(source, destination string) error {
		if destination == third {
			return errors.New("injected replace failure")
		}
		return replaceFile(source, destination)
	}
	err := writeFiles(context.Background(), []File{
		{Path: first, Data: []byte("new-first")},
		{Path: second, Data: []byte("new-second")},
		{Path: third, Data: []byte("new-third")},
	}, ops)
	if err == nil {
		t.Fatal("replace failure unexpectedly succeeded")
	}
	assertFileData(t, first, "old-first")
	assertFileData(t, second, "old-second")
	assertFileData(t, third, "old-third")
}

func TestWriteFilesChmodFailureRollsBack(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	first := filepath.Join(directory, "first.json")
	second := filepath.Join(directory, "second.json")
	if err := os.WriteFile(first, []byte("old-first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("old-second"), 0o600); err != nil {
		t.Fatal(err)
	}

	ops := productionOps()
	ops.chmod = func(name string, mode os.FileMode) error {
		if name == second {
			return errors.New("injected chmod failure")
		}
		return os.Chmod(name, mode)
	}
	err := writeFiles(context.Background(), []File{
		{Path: first, Data: []byte("new-first")},
		{Path: second, Data: []byte("new-second")},
	}, ops)
	if err == nil {
		t.Fatal("chmod failure unexpectedly succeeded")
	}
	assertFileData(t, first, "old-first")
	assertFileData(t, second, "old-second")
}

func TestWriteFilesSyncFailureRollsBackEarlierFiles(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	first := filepath.Join(directory, "first.json")
	second := filepath.Join(directory, "second.json")
	if err := os.WriteFile(first, []byte("old-first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("old-second"), 0o600); err != nil {
		t.Fatal(err)
	}

	ops := productionOps()
	ops.syncDir = func(string) error {
		return errors.New("injected sync failure")
	}
	err := writeFiles(context.Background(), []File{
		{Path: first, Data: []byte("new-first")},
		{Path: second, Data: []byte("new-second")},
	}, ops)
	if err == nil {
		t.Fatal("sync failure unexpectedly succeeded")
	}
	assertFileData(t, first, "old-first")
	assertFileData(t, second, "old-second")
}

func TestReplaceFileOverwritesDestination(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	source := filepath.Join(directory, "source.json")
	destination := filepath.Join(directory, "dest.json")
	if err := os.WriteFile(source, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceFile(source, destination); err != nil {
		t.Fatalf("replaceFile: %v", err)
	}
	assertFileData(t, destination, "replacement")
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists after replace: %v", err)
	}
}

func TestSyncDirectorySucceedsForWritablePath(t *testing.T) {
	t.Parallel()
	if err := syncDirectory(t.TempDir()); err != nil {
		t.Fatalf("syncDirectory: %v", err)
	}
}

func assertFileData(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}
