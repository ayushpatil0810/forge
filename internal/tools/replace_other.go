//go:build !windows

package tools

import (
	"fmt"
	"os"
)

// replaceFile puts src in dst's place. On POSIX, rename replaces the destination
// without unlinking it first.
func replaceFile(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("could not replace %s; the original file was left unchanged: %w", dst, err)
	}
	return nil
}
