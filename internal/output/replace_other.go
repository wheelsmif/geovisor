//go:build !windows

package output

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
