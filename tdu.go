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
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

type file struct { // File information for each scanned item
	path       string
	fullpath   string
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
	fi         os.FileInfo
}

type s_scan struct { // Global variables
	nDenied       int64             // number of access denied
	nItems        int64             // number of scanned items
	nFiles        int64             // number of files
	nDirs         int64             // number of directories
	nSymlinks     int64             // number of symlinks
	nHardlinks    int64             // number of hardlinks
	maxNameLen    int64             // max filename length for depth = 1
	nSockets      int64             // number of sockets
	nCharDevices  int64             // number of character devices
	nBlockDevices int64             // number of block devices
	maxShownLines int64             // number of depth 1 items to display
	maxBigFiles   int64             // number of biggest files to display
	maxWidth      int64             // display width (tty columns)
	reachedDepth  int64             // maximum directory depth reached
	currentDevice uint64            // device number of current partition
	deepestPath   string            // deepest subdirectory reached
	os            string            // operating system
	inodes        map[uint64]string // inode number to file path
	allfiles      []file
	start         time.Time // time at process start
}

func newScanStruct(start time.Time) *s_scan {
	var sc s_scan
	sc.inodes = make(map[uint64]string)
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
	sc.os = runtime.GOOS
	sc.start = start
	return &sc
}

type szDesc []file

func (a szDesc) Len() int           { return len(a) }
func (a szDesc) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a szDesc) Less(i, j int) bool { return a[i].diskUsage > a[j].diskUsage }

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

func fullStat(sc *s_scan, path string, depth int64) (*file, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	sc.nItems++
	fullPath, _ := filepath.Abs(path)
	f := file{path: path, fullpath: fullPath, name: fi.Name(), depth: depth,
		size: fi.Size(), isDir: fi.IsDir(), blockSize: 4096, fi: fi}
	// Firstly, disk usage is estimated with a block size of 4kb,
	// then it will be precisely calculated with a native syscall.
	f.diskUsage = avgDiskUsage(f.size, f.blockSize)

	if depth > sc.reachedDepth {
		sc.reachedDepth = depth
		sc.deepestPath = filepath.Dir(f.fullpath)
	}

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
		fmt.Printf("  Socket: [%s]\n", f.fullpath)
		sc.nSockets++
		f.isSpecial = true

	default:
		fmt.Printf("  Unknown file type (%v): [%s]\n", mode, f.fullpath)
	}
	err = sysStat(sc, &f)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func printFileTypes(sc *s_scan) { // Summary of file types with non-zero counter
	fmt.Printf("  Item: %d, Dir: %d, File: %d", sc.nItems, sc.nDirs, sc.nFiles)
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
	fmt.Printf(", Depth: %d\n", sc.reachedDepth)
	fmt.Printf("  Deepest: %s", sc.deepestPath)
}

func smartTruncate(name string, max int64) string { // cut in the middle
	l := int64(len(name))
	if l <= max || max < 10 {
		return name
	}
	start := max/2 - 4
	end := max - (start + 1)
	cut := name[0:start] + "~" + name[l-end:]
	return cut
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
	strfmt += "\n"
	fmt.Printf(strfmt, "TOTAL", fmtSz(total.diskUsage))
	fmt.Printf(strfmt, "Apparent size", fmtSz(total.size))
	fmt.Println()
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

func showElapsed(sc *s_scan) {
	elapsed := time.Since(sc.start)
	fmt.Printf("\n  Total time: %.3f s\n\n", elapsed.Seconds())
}

/* Basically, the process has got several steps:
 * 1. change directory to given path
 * 2. scan all files recursively, collecting 'stat' data
 * 3. sort results and output a list of biggest items at depth 1.
 * 4. show the largest files at any depth.
 */
func main() {
	start := time.Now()
	fmt.Println("\n=========== Top Disk Usage v1.22 (GNU GPL) ===========\n")
	d := usage() // Step 1
	sc := newScanStruct(start)
	fmt.Println("  Operating system: " + sc.os)
	fmt.Printf("  Scanning [%s]...\n", d)
	var fi []file
	t, _ := scan(sc, &fi, ".", 1) // Step 2
	show(sc, fi, t)               // Step 3
	showmax(sc, t)                // step 4
	showElapsed(sc)
}
