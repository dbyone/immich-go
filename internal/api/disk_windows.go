//go:build windows

package api

import (
	"fmt"
	"syscall"
	"unsafe"
)

type diskUsage struct {
	Total     int64
	Used      int64
	Available int64
}

func (d diskUsage) UsagePercent() float64 {
	if d.Total == 0 {
		return 0
	}
	return float64(d.Used) / float64(d.Total) * 100
}

func diskStats(root string) (diskUsage, error) {
	p, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return diskUsage{}, err
	}
	var free, total, avail int64
	dll := syscall.NewLazyDLL("kernel32.dll")
	proc := dll.NewProc("GetDiskFreeSpaceExW")
	rc, _, err := proc.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&free)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&avail)),
	)
	if rc == 0 {
		return diskUsage{}, fmt.Errorf("GetDiskFreeSpaceExW: %w", err)
	}
	return diskUsage{Total: total, Available: free, Used: total - free}, nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
