package seerr

import (
	"os"
	"syscall"
)

// diskStats returns free/total bytes and whether path is a writable directory.
// On any error it reports zeros + accessible=false so Seerr surfaces the
// misconfiguration rather than failing silently.
func diskStats(path string) (free, total int64, accessible bool) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return 0, 0, false
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, true // exists, but stats unavailable
	}
	free = int64(st.Bavail) * int64(st.Bsize)
	total = int64(st.Blocks) * int64(st.Bsize)
	return free, total, true
}
