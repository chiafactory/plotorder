package disk

import (
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func GetAvailableSpace(directory string) (uint64, string, error) {
	h := windows.MustLoadDLL("kernel32.dll")
	c := h.MustFindProc("GetDiskFreeSpaceExW")

	var freeBytes, totalBytes, availableBytes int64
	_, _, err := c.Call(
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(directory))),
		uintptr(unsafe.Pointer(&freeBytes)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&availableBytes)))
	if err.(windows.Errno) == 0 {
		return uint64(freeBytes), filepath.VolumeName(directory), nil
	}
	return 0, "", err
}
