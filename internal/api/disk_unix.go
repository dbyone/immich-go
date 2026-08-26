//go:build !windows

package api

import (
	"fmt"
	"syscall"
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
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil {
		return diskUsage{}, err
	}
	free := int64(st.Bavail) * int64(st.Bsize)
	total := int64(st.Blocks) * int64(st.Bsize)
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
