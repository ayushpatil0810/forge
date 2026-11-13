//go:build windows

package audit

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procLockFileEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("LockFileEx")
var procUnlockFileEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("UnlockFileEx")

func lockExclusive(f *os.File) error {
	var ol windows.Overlapped
	r, _, err := procLockFileEx.Call(
		f.Fd(),
		uintptr(0x00000002), // LOCKFILE_EXCLUSIVE_LOCK
		0,
		1, 0,
		uintptr(unsafe.Pointer(&ol)),
	)
	if r == 0 {
		if err != nil {
			return err
		}
		return windows.ERROR_LOCK_FAILED
	}
	return nil
}

func unlockExclusive(f *os.File) error {
	var ol windows.Overlapped
	r, _, err := procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	if r == 0 {
		if err != nil {
			return err
		}
		return windows.ERROR_LOCK_FAILED
	}
	return nil
}
