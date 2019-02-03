/* Top Disk Usage.
 * Copyright (C) 2019 Joseph Paul <joseph.paul1@gmx.com>
 * https://bitbucket.org/josephpaul0/tdu
 *
 * This program is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation; either version 2 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License along
 * with this program; if not, write to the Free Software Foundation, Inc.,
 * 51 Franklin Street, Fifth Floor, Boston, MA 02110-1301 USA.
 */

/* This program estimates the disk usage of all files in a given path and then
 * displays a sorted list of the biggest items.  The estimation method used is
 * similar to the 'du -skx' command from GNU Coreutils package.
 */

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type file struct { // File information for each scanned item
	path       string
	name       string
	isRegular  bool
	isDir      bool
	isSymlink  bool
	isOtherFs  bool
	isSpecial  bool
	size       int64
	diskUsage  int64
	depth      int64
	items      int64
	blockSize  int64
	nBlocks512 int64 // number of 512byte blocks
	inode      uint64
	nLinks     uint64
	deviceId   uint64
}

type s_scan struct { // Global variables
	nDenied       int64             // number of access denied
	nFiles        int64             // number of files
	nDirs         int64             // number of directories
	nSymlinks     int64             // number of symlinks
	nHardlinks    int64             // number of hardlinks
	maxNameLen    int64             // max filename length for depth = 1
	nSockets      int64             // number of sockets
	nCharDevices  int64             // number of character devices
	nBlockDevices int64             // number of block devices
	inodeMap      map[uint64]string // inode number to file path
	currentDevice uint64            // device number of current partition
	maxShownLines int64             // number of depth 1 items to display
	maxBigFiles   int64             // number of biggest files to display
	maxWidth      int64             // display width (tty columns)
	allfiles      []file
}

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

func newScanStruct() *s_scan {
	var sc s_scan
	sc.inodeMap = make(map[uint64]string)
	sc.maxShownLines = 15 // should be configurable
	sc.maxBigFiles = 7    // should be configurable
	sc.maxWidth = 80
	w := getTtyWidth()
	if w >= 72 {
		if w <= 120 {
			sc.maxWidth = w
		} else {
			sc.maxWidth = 120
		}
	}
	sc.maxNameLen = sc.maxWidth - 43 // formatting: stay below N columns
	return &sc
}

type szDesc []file

func (a szDesc) Len() int           { return len(a) }
func (a szDesc) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a szDesc) Less(i, j int) bool { return a[i].diskUsage > a[j].diskUsage }

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

func fmtSz(size int64) string { // Formats in kilobytes
	var sz = float64(size)
	var power float64 = 2.0
	unit := "Kb"
	sz /= math.Pow(1024, power-1)
	return fmt.Sprintf("%9d %s", int64(sz), unit)
}

// Fallback to approximate disk usage
func avgDiskUsage(sz, bsize int64) int64 {
	if sz == 0 {
		return 0
	}
	if sz < bsize {
		return bsize
	}
	d := sz / bsize
	i := d * bsize
	if sz > i {
		return i + bsize
	}
	return sz
}

func linuxStat(fi os.FileInfo, f *file) error {
	sys := fi.Sys()
	if sys == nil {
		panic("Stat System Interface Not Available ! Linux is required")
	}
	stat, ok := sys.(*syscall.Stat_t)
	if !ok {
		err := errors.New("syscall.Stat_t undefined, Linux is required")
		return err
	}
	f.deviceId = stat.Dev
	f.inode = stat.Ino
	f.nLinks = stat.Nlink
	f.blockSize = stat.Blksize
	f.nBlocks512 = stat.Blocks
	f.diskUsage = 512 * f.nBlocks512
	return nil
}

func fullStat(sc *s_scan, path string, depth int64) (*file, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	fullPath, _ := filepath.Abs(path)
	f := file{path: path, name: fi.Name(), depth: depth, size: fi.Size(),
		isDir: fi.IsDir(), blockSize: 4096}
	// Firstly, disk usage is estimated with a block size of 4kb,
	// then it will be precisely calculated with a native syscall.
	f.diskUsage = avgDiskUsage(f.size, f.blockSize)

	switch mode := fi.Mode(); {
	case mode.IsRegular():
		f.isRegular = true
		sc.nFiles++

	case mode.IsDir():
		sc.nDirs++

	case mode&os.ModeSymlink != 0: // True if the file is a symlink.
		sc.nSymlinks++
		f.isSymlink = true
		if f.size < 60 { // On Linux, fast symlinks are stored in inode
			f.diskUsage = 0
		}

	case mode&os.ModeNamedPipe != 0:
		fmt.Printf("  Named pipe: [%s]\n", f.path)
		f.isSpecial = true

	case mode&os.ModeCharDevice != 0:
		//fmt.Printf("  Character Device: [%s]\n", f.path)
		sc.nCharDevices++
		f.isSpecial = true

	case mode&os.ModeDevice != 0:
		//fmt.Printf("  Block device: [%s]\n", f.path)
		sc.nBlockDevices++
		f.isSpecial = true

	case mode&os.ModeSocket != 0:
		fmt.Printf("  Socket: [%s]\n", fullPath)
		sc.nSockets++
		f.isSpecial = true

	default:
		fmt.Printf("  Unknown file type (%v): [%s]\n", mode, fullPath)
	}

	err = linuxStat(fi, &f)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	if depth == 1 {
		sc.currentDevice = f.deviceId
		fmt.Printf("  Partition: %s\n\n", getPartition(sc.currentDevice))
	}
	if f.deviceId != sc.currentDevice {
		f.isOtherFs = true
		fmt.Printf("  Not crossing FS boundary at %-15s %s\n",
			fullPath, getPartition(f.deviceId))
	}
	_, ok := sc.inodeMap[f.inode]
	if ok { // Hardlink means inode used more than once in map
		if !f.isOtherFs { // Other FS may have a same inode number (root=2)
			f.diskUsage = 0
			sc.nHardlinks++
			//orig := sc.ino[f.ino]
			//fmt.Printf("Hardlink (ino=%d): %s [%s]\n", f.ino, fullPath, orig)
		}
	} else { // path from the first occurence of inode is saved
		sc.inodeMap[f.inode] = fullPath
	}
	return &f, nil
}

func printFileTypes(sc *s_scan) { // Summary of file types with non-zero counter
	fmt.Printf("\n\n  ")
	fmt.Printf("Dir: %d, File: %d", sc.nDirs, sc.nFiles)
	if sc.nSymlinks > 0 {
		fmt.Printf(", Symlink: %d", sc.nSymlinks)
	}
	if sc.nHardlinks > 0 {
		fmt.Printf(", Hardlink: %d", sc.nHardlinks)
	}
	if sc.nSockets > 0 {
		fmt.Printf(", Socket: %d", sc.nSockets)
	}
	if sc.nDenied > 0 {
		fmt.Printf(", Denied: %d", sc.nDenied)
	}
	if sc.nBlockDevices > 0 {
		fmt.Printf(", Block device: %d", sc.nBlockDevices)
	}
	if sc.nCharDevices > 0 {
		fmt.Printf(", Character device: %d", sc.nCharDevices)
	}
}

func smartTruncate(name string, max int64) string { // cut in the middle
	l := int64(len(name))
	if l <= max {
		return name
	}
	start := max/2 - 4
	end := max - (start + 1)
	p1 := name[0:start]
	p2 := "~"
	p3 := name[l-end:]
	name = p1 + p2 + p3
	return name
}

func scan(sc *s_scan, files *[]file, path string, depth int64) (*file, error) {
	f, err := fullStat(sc, path, depth)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	if f.isOtherFs {
		return f, nil
	}
	if f.isSymlink || !f.isDir {
		if files != nil {
			*files = append(*files, *f)
		}
		sc.allfiles = append(sc.allfiles, *f)
		return f, nil
	}

	fs, err := ioutil.ReadDir(path)
	if err != nil {
		sc.nDenied++
	}
	var size, du, items int64 = f.size, f.diskUsage, 0
	var ptr *[]file
	for _, i := range fs { // Calculate total size by recursive scanning
		ptr = files
		if depth > 1 {
			ptr = nil // Forget details for deep directories
		}
		items++
		cf, err := scan(sc, ptr, path+"/"+i.Name(), depth+1)
		if err != nil {
			fmt.Println(err)
			continue
		}
		size += cf.size
		du += cf.diskUsage
		items += cf.items
	}
	fo := file{path: path, name: f.name, size: size, diskUsage: du,
		isDir: true, depth: depth, items: items}
	if depth > 1 && files != nil {
		*files = append(*files, fo)
	}
	return &fo, nil
}

func showmax(sc *s_scan, total *file) {
	sort.Sort(szDesc(sc.allfiles)) // sort biggest files by descending size
	fmt.Println()
	fmt.Println("  --------- BIGGEST FILES -------------")
	var i, sum, rsum int64 = 0, 0, 0
	fi := sc.allfiles
	for _, f := range fi {
		i++
		if i > sc.maxBigFiles {
			rsum += f.diskUsage
			continue
		}
		f.path = f.path[2:]
		f.path = smartTruncate(f.path, sc.maxNameLen+18)
		fmt.Printf("%3d.%s| %s\n", i, fmtSz(f.diskUsage), f.path)
		sum += f.diskUsage
	}
	x := "  =%13s| %.02f%% of total disk usage\n"
	p := float64(sum*100.0) / float64(total.diskUsage)
	fmt.Printf(x, fmtSz(sum), p)
}

func show(sc *s_scan, fi []file, total *file) {
	sort.Sort(szDesc(fi))    // sort files and folders by descending size
	var fmtNameLen int64 = 7 // minimum for the total line
	var rDiskUsage int64 = 0 // remaining disk usage
	var rItems int64 = 0     // remaining items
	var i int64 = 0
	fmt.Println()
	for _, f := range fi { // Totals and max len loop
		i++
		if i > sc.maxShownLines {
			rDiskUsage += f.diskUsage
			rItems += f.items
			if f.isDir {
				rItems++
			}
			continue
		}
		l := int64(len(f.name))
		if f.isDir {
			l++
		}
		if l > fmtNameLen {
			fmtNameLen = l
		}
	}
	fmtNameLen++
	if fmtNameLen >= sc.maxNameLen {
		fmtNameLen = sc.maxNameLen
	}
	var strfmt = "%3d. %" + fmt.Sprintf("%d", fmtNameLen) + "s | %s|%6.2f%%"
	i = 0
	for _, f := range fi {
		if !f.isDir && sc.nFiles == 0 { // ignore special files
			continue
		}
		i++
		if i > sc.maxShownLines { // stop
			break
		}
		if f.isDir {
			f.name += "/"
		}
		f.name = smartTruncate(f.name, sc.maxNameLen)
		var p float64 = 0
		if total.diskUsage > 0 {
			p = float64(f.diskUsage*100.0) / float64(total.diskUsage)
		}
		fmt.Printf(strfmt, i, f.name, fmtSz(f.diskUsage), p)
		if f.isDir {
			fmt.Printf("| %6d items", f.items)
		}
		fmt.Println()
	}
	fmtNameLen += 5 // The line number width
	strfmt = "%" + fmt.Sprintf("%d", fmtNameLen) + "s | %s|"
	if rDiskUsage > 0 {
		p := float64(rDiskUsage*100.0) / float64(total.diskUsage)
		s := strfmt + "%6.2f%%| %6d items\n"
		fmt.Printf(s, "REMAINING", fmtSz(rDiskUsage), p, rItems)
	}
	fmt.Printf(strfmt, "TOTAL", fmtSz(total.diskUsage))
	printFileTypes(sc)
	fmt.Println()
}

/* Check command line arguments and change working directory if needed */
func usage() string {
	args := os.Args[1:]
	var dir string
	if len(args) == 0 { // return "current directory"
		dir, _ = os.Getwd()
		return dir
	}
	if len(args) > 1 {
		fmt.Printf("Usage:\n\t%v <path>\n\n", os.Args[0])
		os.Exit(2)
	}
	err := os.Chdir(args[0])
	if err != nil {
		fmt.Printf("ERROR: Cannot change directory to %s\n%v\n\n", args[0], err)
		os.Exit(2)
	}
	dir, err = os.Getwd()
	if err != nil {
		panic(err)
	}
	return dir
}

/* Basically, the process has got 3 steps:
 * 1. change directory to given path
 * 2. scan all files recursively, collecting 'stat' data
 * 3. sort results and output a list of biggest items.
 */
func main() {
	fmt.Println("\n=========== Top Disk Usage v1.20 (GNU GPL) ===========\n")
	fmt.Println("  Operating system: " + runtime.GOOS)
	d := usage() // Step 1
	start := time.Now()
	sc := newScanStruct()
	var fi []file
	fmt.Printf("  Scanning [%s]...\n", d)
	t, _ := scan(sc, &fi, ".", 1) // Step 2
	show(sc, fi, t)               // Step 3
	showmax(sc, t)
	elapsed := time.Since(start)
	fmt.Printf("\n  Total time: %.3f s\n\n", elapsed.Seconds())
}
