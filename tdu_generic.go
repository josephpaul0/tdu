// +build !linux

/* Generic functions for non-Linux OS */

package main

// Console width is not yet computed on non-Linux OS
func getTtyWidth() int64 {
	return 80
}

// Disk usage is inaccurate because appropriate syscall is not yet implemented
func sysStat(sc *s_scan, f *file) error {
	f.deviceId = 0
	f.inode = 0
	f.nLinks = 0
	f.blockSize = 4096
	f.nBlocks512 = 0
	f.diskUsage = f.size
	return nil
}
