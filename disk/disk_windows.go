package disk

import (
	"os"

	"golang.org/x/sys/windows"
)

func GetAvailableSpace(directory string) (uint64, string, error) {
	h := windows.MustLoadDLL("kernel32.dll")
	c := h.MustFindProc("GetDiskFreeSpaceExW")

	var freeBytes int64
	_, _, err := c.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(directory))),
		uintptr(unsafe.Pointer(&freeBytes)), nil, nil)
	return uint64(freeBytes), filepath.VolumeName(directory), nil
}
