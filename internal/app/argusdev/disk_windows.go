//go:build windows

package argusdev

import "golang.org/x/sys/windows"

func availableDiskBytes(path string) (uint64, error) {
	directory, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(directory, &available, &total, &free); err != nil {
		return 0, err
	}
	return available, nil
}
