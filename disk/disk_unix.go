// +build linux darwin

package disk

import (
	"strconv"

	"golang.org/x/sys/unix"
)

func GetAvailableSpace(directory string) (uint64, string, error) {
	var statfs unix.Statfs_t
	if err := unix.Statfs(directory, &statfs); err != nil {
		return 0, "", err
	}

	var stat unix.Stat_t
	if err := unix.Stat(directory, &stat); err != nil {
		return 0, "", err
	}
	return statfs.Bavail * uint64(statfs.Bsize), strconv.Itoa(int(stat.Dev)), nil
}
