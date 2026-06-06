//go:build windows

package api

import (
	"syscall"
	"unsafe"

	"github.com/video-site/backend/internal/storageusage"
)

func localDiskStats(path string) (storageusage.DiskStats, error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return storageusage.DiskStats{}, err
	}
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetDiskFreeSpaceExW")
	var freeBytesAvailable, totalBytes, totalFreeBytes int64
	r, _, e := proc.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if r == 0 {
		return storageusage.DiskStats{}, e
	}
	return storageusage.DiskStats{
		AvailableBytes: freeBytesAvailable,
		CapacityBytes:  totalBytes,
	}, nil
}
