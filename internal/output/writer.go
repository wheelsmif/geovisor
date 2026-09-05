// Package output writes generated artifacts without exposing partial files.
package output

import (
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

// WriteFiles atomically replaces each file after every artifact has been
// staged successfully. Callers must finish all generation before calling it.
func WriteFiles(files []File) error {
	if err := validateFiles(files); err != nil {
		return err
	}

	staged := make([]stagedFile, 0, len(files))
	cleanup := func() {
		for _, file := range staged {
			if file.temp != "" {
				_ = os.Remove(file.temp)
			}
		}
	}
	defer cleanup()

	for _, file := range files {
		parent := filepath.Dir(file.Path)
		if err := os.MkdirAll(parent, directoryPermission); err != nil {
			return fmt.Errorf("create output directory %q: %w", parent, err)
		}
		temp, err := stageFile(parent, filepath.Base(file.Path), file.Data)
		if err != nil {
			return fmt.Errorf("stage output %q: %w", file.Path, err)
		}
		staged = append(staged, stagedFile{target: file.Path, temp: temp})
	}

	for index := range staged {
		if err := replaceFile(staged[index].temp, staged[index].target); err != nil {
			return fmt.Errorf("replace output %q: %w", staged[index].target, err)
		}
		staged[index].temp = ""
		if err := os.Chmod(staged[index].target, filePermission); err != nil {
			return fmt.Errorf("secure output permissions %q: %w", staged[index].target, err)
		}
	}
	return nil
}

func validateFiles(files []File) error {
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
