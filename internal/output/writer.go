// Package output writes generated artifacts without exposing a mixed set of
// old and new files. Each file is replaced atomically. A replace, chmod, or
// parent-directory sync failure restores destinations this call already
// replaced.
package output

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	filePermission      = 0o600
	directoryPermission = 0o750
)

// File is one fully staged output artifact.
type File struct {
	Path string
	Data []byte
}

type stagedFile struct {
	target string
	temp   string
}

type replacement struct {
	target   string
	backup   string
	replaced bool
}

type fileOps struct {
	mkdirAll func(path string, perm os.FileMode) error
	chmod    func(name string, mode os.FileMode) error
	replace  func(source, destination string) error
	syncDir  func(path string) error
}

func productionOps() fileOps {
	return fileOps{
		mkdirAll: os.MkdirAll,
		chmod:    os.Chmod,
		replace:  replaceFile,
		syncDir:  syncDirectory,
	}
}

// WriteFiles stages every artifact, then replaces each destination. A failure
// during replace, chmod, or parent-directory sync restores any destination this
// call already replaced. Parent directories are synced before backups are
// discarded so a sync error does not leave new files behind an error return.
// Callers must finish all generation before calling.
func WriteFiles(ctx context.Context, files []File) error {
	return writeFiles(ctx, files, productionOps())
}

func writeFiles(ctx context.Context, files []File, ops fileOps) error {
	if ctx == nil {
		return errors.New("output context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write files: %w", err)
	}
	if err := ValidateFiles(files); err != nil {
		return err
	}

	staged := make([]stagedFile, 0, len(files))
	replacements := make([]replacement, 0, len(files))
	committed := false
	cleanup := func() {
		if committed {
			return
		}
		for _, file := range staged {
			if file.temp != "" {
				_ = os.Remove(file.temp)
			}
		}
		for _, item := range replacements {
			if item.replaced {
				if item.backup != "" {
					_ = ops.replace(item.backup, item.target)
				} else {
					_ = os.Remove(item.target)
				}
			} else if item.backup != "" {
				_ = os.Remove(item.backup)
			}
		}
	}
	defer cleanup()

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("write files: %w", err)
		}
		parent := filepath.Dir(file.Path)
		if err := ops.mkdirAll(parent, directoryPermission); err != nil {
			return fmt.Errorf("create output directory %q: %w", parent, err)
		}
		temp, err := stageFile(parent, filepath.Base(file.Path), file.Data)
		if err != nil {
			return fmt.Errorf("stage output %q: %w", file.Path, err)
		}
		staged = append(staged, stagedFile{target: file.Path, temp: temp})
	}

	parents := make([]string, 0, len(staged))
	seenParent := make(map[string]struct{}, len(staged))
	for index := range staged {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("write files: %w", err)
		}
		item := replacement{target: staged[index].target}
		info, statErr := os.Stat(staged[index].target)
		switch {
		case statErr == nil && !info.IsDir():
			backup, err := backupExisting(staged[index].target)
			if err != nil {
				return fmt.Errorf("backup output %q: %w", staged[index].target, err)
			}
			item.backup = backup
		case statErr == nil:
			// Destination exists as a directory; replace will fail and rollback
			// earlier files. Do not treat it as a file backup.
		case os.IsNotExist(statErr):
		default:
			return fmt.Errorf("stat output %q: %w", staged[index].target, statErr)
		}
		if err := ops.replace(staged[index].temp, staged[index].target); err != nil {
			replacements = append(replacements, item)
			return fmt.Errorf("replace output %q: %w", staged[index].target, err)
		}
		staged[index].temp = ""
		item.replaced = true
		replacements = append(replacements, item)
		if err := ops.chmod(item.target, filePermission); err != nil {
			return fmt.Errorf("secure output permissions %q: %w", item.target, err)
		}
		parent := filepath.Dir(item.target)
		if _, seen := seenParent[parent]; !seen {
			seenParent[parent] = struct{}{}
			parents = append(parents, parent)
		}
	}

	for _, parent := range parents {
		if err := ops.syncDir(parent); err != nil {
			return fmt.Errorf("sync output directory %q: %w", parent, err)
		}
	}
	for _, item := range replacements {
		if item.backup != "" {
			_ = os.Remove(item.backup)
		}
	}
	replacements = replacements[:0]
	committed = true
	return nil
}

// ValidateFiles rejects empty paths and colliding destinations. The CLI uses
// the same check before WriteFiles so --format all can fail before any replace.
func ValidateFiles(files []File) error {
	seen := make(map[string]string, len(files))
	for index, file := range files {
		if strings.TrimSpace(file.Path) == "" {
			return fmt.Errorf("files[%d].path must not be empty", index)
		}
		absolute, err := filepath.Abs(filepath.Clean(file.Path))
		if err != nil {
			return fmt.Errorf("resolve output path %q: %w", file.Path, err)
		}
		key := absolute
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if previous, exists := seen[key]; exists {
			return fmt.Errorf("output path collision: %q and %q", previous, file.Path)
		}
		seen[key] = file.Path
	}
	return nil
}

func backupExisting(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return stageFile(filepath.Dir(path), filepath.Base(path)+".prev", data)
}

func stageFile(directory, base string, data []byte) (path string, returnedError error) {
	file, err := os.CreateTemp(directory, "."+base+".geovisor-*")
	if err != nil {
		return "", err
	}
	path = file.Name()
	defer func() {
		if returnedError != nil {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(filePermission); err != nil {
		return "", err
	}
	if err := writeAll(file, data); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func writeAll(file *os.File, data []byte) error {
	for len(data) > 0 {
		written, err := file.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return errors.New("write made no progress")
		}
		data = data[written:]
	}
	return nil
}
