//go:build windows

package tools

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

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

func resolveLink(path string) (string, error) {
	if isReparsePoint(path) {
		target, err := reparseTarget(path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		return filepath.Clean(target), nil
	}
	return filepath.EvalSymlinks(path)
}

func reparseTarget(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(
		p, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	buf := make([]byte, 16*1024)
	var n uint32
	if err := windows.DeviceIoControl(h, windows.FSCTL_GET_REPARSE_POINT, nil, 0, &buf[0], uint32(len(buf)), &n, nil); err != nil {
		return "", err
	}
	if n < 20 {
		return "", fmt.Errorf("reparse data for %q is too short", path)
	}
	tag := binary.LittleEndian.Uint32(buf[0:4])
	subOff := int(binary.LittleEndian.Uint16(buf[8:10]))
	subLen := int(binary.LittleEndian.Uint16(buf[10:12]))
	start := 16
	if tag == windows.IO_REPARSE_TAG_SYMLINK {
		start = 20
	}
	if start+subOff+subLen > int(n) {
		return "", fmt.Errorf("reparse data for %q is truncated", path)
	}
	raw := buf[start+subOff : start+subOff+subLen]
	u := make([]uint16, subLen/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(raw[i*2 : i*2+2])
	}
	name := string(utf16.Decode(u))
	if tag == windows.IO_REPARSE_TAG_SYMLINK && binary.LittleEndian.Uint32(buf[16:20])&1 != 0 {
		return strings.TrimRight(name, `\`), nil
	}
	return cleanNTPath(name), nil
}

func cleanNTPath(p string) string {
	p = strings.TrimPrefix(p, `\??\`)
	if len(p) >= 4 && strings.EqualFold(p[:4], `UNC\`) {
		p = `\\` + p[4:]
	}
	p = strings.TrimRight(p, `\`)
	if len(p) == 2 && p[1] == ':' {
		p += `\`
	}
	return p
}

func isReparsePoint(path string) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return false
	}
	return attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
