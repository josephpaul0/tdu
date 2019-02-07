// +build linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

func getTtyWidth() int64 {
	wss := struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}{}
	ws := &wss
	stdin := uintptr(syscall.Stdin)
	cmd := uintptr(syscall.TIOCGWINSZ)
	p := uintptr(unsafe.Pointer(ws))
	ret, _, errno := syscall.Syscall(syscall.SYS_IOCTL, stdin, cmd, p)
	if int(ret) == -1 {
		panic(errno)
	}
	//fmt.Printf("  TTY cols=%d lines=%d\n", ws.Col, ws.Row)
	return int64(ws.Col)
}

/* On Linux, try to find the partition name from the device number */
func getPartition(dev uint64) string {
	file, err := os.Open("/proc/partitions")
	if err != nil {
		panic(err)
	}
	defer file.Close()
	name := fmt.Sprintf("[dev 0x%04X]", dev)
	high := (dev >> 8) & 0xff
	low := dev & 0xff
	scanner := bufio.NewScanner(file)
	// Format of lines should be "major minor  #blocks  name"
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) != 4 {
			continue // ignore lines without 4 fields (see format above)
		}
		for i := 0; i < 4; i++ {
			h, _ := strconv.Atoi(fields[0]) // get major
			l, _ := strconv.Atoi(fields[1]) // get minor
			if h == int(high) && l == int(low) {
				name = fmt.Sprintf("(%d,%d) [/dev/%s]", h, l, fields[3])
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	return name
}

func sysStat(sc *s_scan, f *file) error {
	sys := f.fi.Sys()
	if sys == nil {
		panic("Stat System Interface Not Available ! Linux is required")
	}
	stat, ok := sys.(*syscall.Stat_t)
	if !ok {
		panic("syscall.Stat_t undefined, Linux is required")
	}
	f.deviceId = stat.Dev
	f.inode = stat.Ino
	f.nLinks = stat.Nlink
	f.blockSize = stat.Blksize
	f.nBlocks512 = stat.Blocks
	f.diskUsage = 512 * f.nBlocks512
	if f.depth == 1 {
		sc.currentDevice = f.deviceId
		fmt.Printf("  Partition: %s\n", getPartition(sc.currentDevice))
	}
	if f.deviceId != sc.currentDevice {
		f.isOtherFs = true
		fmt.Printf("  Not crossing FS boundary at %-15s %s\n",
			f.fullpath, getPartition(f.deviceId))
	}
	_, ok = sc.inodes[f.inode]
	if ok { // Hardlink means inode used more than once in map
		if !f.isOtherFs { // Other FS may have a same inode number (root=2)
			f.diskUsage = 0
			sc.nHardlinks++
			//orig := sc.ino[f.ino]
			//fmt.Printf("Hardlink (ino=%d): %s [%s]\n", f.ino, f.fullpath, orig)
		}
	} else { // path from the first occurence of inode is saved
		sc.inodes[f.inode] = f.fullpath
	}
	return nil
}
