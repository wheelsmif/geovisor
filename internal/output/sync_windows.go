//go:build windows

package output

// NTFS commits the directory entry with MOVEFILE_WRITE_THROUGH, which
// replaceFile already requests. A directory handle cannot be flushed here.
func syncDirectory(string) error {
	return nil
}
