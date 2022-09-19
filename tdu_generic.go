// +build !linux
// +build !windows
// +build !freebsd

/* Top Disk Usage.
 * Copyright (C) 2019 Joseph Paul <joseph.paul1@gmx.com>
 * https://github.com/josephpaul0/tdu
 *
 * This program is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation; either version 2 of the License, or
 * (at your option) any later version.
 */

/* Generic functions for non-Linux OS */

package main

import (
	"fmt"
)

func osInit() bool {
	return true
}
func osEnd() bool {
	return true
}

// Console width is fixed on other systems
func getTtyWidth(sc *s_scan) int {
	return 80
}

func initTty(sc *sc_scan) {} // OS Specific

func printAlert(sc *s_scan, msg string) {
	fmt.Printf(msg)
}

func printProgress(sc *s_scan) {
	n := sc.nErrors + sc.nItems
	fmt.Printf("  [.... scanning... %6d  ....]\r", n)
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
