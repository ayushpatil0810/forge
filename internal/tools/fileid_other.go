//go:build !windows

package tools

import "syscall"

func statIdentity(path string) (fileKey, uint64, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return fileKey{}, 0, err
	}
	return fileKey{dev: uint64(st.Dev), ino: uint64(st.Ino), ok: true}, uint64(st.Nlink), nil
}
