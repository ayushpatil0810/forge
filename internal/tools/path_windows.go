//go:build windows

package tools

import (
	"os"

	"golang.org/x/sys/windows"
)

func expandLongPath(p string) string {
	if p == "" {
		return p
	}
	if _, err := os.Lstat(p); err != nil {
		return p
	}
	ptr, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	buf := make([]uint16, 32768)
	n, err := windows.GetLongPathName(ptr, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return p
	}
	return windows.UTF16ToString(buf[:n])
}
