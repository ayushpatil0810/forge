//go:build windows

package tools

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// replaceFile puts src in dst's place without deleting dst first.
// MoveFileEx with MOVEFILE_REPLACE_EXISTING is atomic on the same volume.
func replaceFile(src, dst string) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
	if err != nil {
		return fmt.Errorf("could not replace %s (it may be open or read-only); the original file was left unchanged: %w", dst, err)
	}
	return nil
}
