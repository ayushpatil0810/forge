//go:build windows

package tools

import (
	"os"

	"golang.org/x/sys/windows"
)

func statIdentity(path string) (fileKey, uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return fileKey{}, 0, err
	}
	defer f.Close()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return fileKey{}, 0, err
	}
	return fileKey{
		dev: uint64(info.VolumeSerialNumber),
		ino: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
		ok:  true,
	}, uint64(info.NumberOfLinks), nil
}
