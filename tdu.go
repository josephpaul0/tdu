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
	"flag"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	prg_VERSION       = "1.28"
	dft_MAXSHOWNLINES = 15
	dft_MAXBIGFILES   = 7
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
	readError  bool
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

type ino_map map[uint64]uint16 // map of inode number and counter

type s_scan struct { // Global variables
	nErrors       int64    // number of Lstat errors
	nDenied       int64    // number of access denied
	nItems        int64    // number of scanned items
	nFiles        int64    // number of files
	nDirs         int64    // number of directories
	nSymlinks     int64    // number of symlinks
	nHardlinks    int64    // number of hardlinks
	maxNameLen    int64    // max filename length for depth = 1
	nSockets      int64    // number of sockets
	nCharDevices  int64    // number of character devices
	nBlockDevices int64    // number of block devices
	maxWidth      int64    // display width (tty columns)
	reachedDepth  int64    // maximum directory depth reached
	maxPathLen    int64    // maximum directory path length
	maxFNameLen   int64    // maximum filename length
	currentDevice uint64   // device number of current partition
	maxShownLines int      // number of depth 1 items to display
	maxBigFiles   int      // number of biggest files to display
	wsl           bool     // Windows Subsystem for Linux
	partinfo      bool     // found info about partition
	foundBoundary bool     // found other filesystems
	hideMax       bool     // hide deepest and longest paths
	export        bool     // export result to Ncdu's JSON format
	exportPath    string   // path to exported file
	exportFile    *os.File // exported file
	deepestPath   string   // deepest subdirectory reached
	longestPath   string   // longest directory path
	longestFName  string   // longest filename
	os            string   // operating system
	fsType        string   // FS type from /proc/mounts
	partition     string   // current partition
	mountOptions  string   // mount options from /proc/mounts
	pathSeparator string   // os.PathSeparator as string
	inodes        ino_map  // inode number to file path
	bigfiles      []file
	start         time.Time // time at process start
}

func detectOS(sc *s_scan) {
	sc.os = runtime.GOOS
	if sc.os != "linux" {
		return
	}
	// Try to detect if we are on Windows 10 Subsystem for Linux
	b, err := ioutil.ReadFile("/proc/version")
	if err != nil {
		panic(err)
	}
	s := string(b)
	if strings.Contains(s, "Microsoft") {
		sc.wsl = true
		sc.os = "WSL"
	}
}

func getConsoleWidth(sc *s_scan) {
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
}

func newScanStruct(start time.Time) *s_scan {
	var sc s_scan
	sc.pathSeparator = string(os.PathSeparator)
	sc.inodes = make(map[uint64]uint16, 256)
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
	return fmt.Sprintf("%d %s", int64(sz), unit)
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
		sc.nErrors++
		// fmt.Println(err)
		return nil, err
	}
	sc.nItems++
	wd, _ := os.Getwd()
	var fullPath string
	if wd == "/" {
		fullPath = wd + path
	} else {
		fullPath = wd + sc.pathSeparator + path
	}
	f := file{path: path, fullpath: fullPath, name: fi.Name(), depth: depth,
		size: fi.Size(), isDir: fi.IsDir(), blockSize: 4096, fi: fi}
	// Firstly, disk usage is estimated with a block size of 4kb,
	// then it will be precisely calculated with a native syscall.
	f.diskUsage = avgDiskUsage(f.size, f.blockSize)

	l := int64(len(fullPath))
	if f.isDir && l > sc.maxPathLen {
		sc.maxPathLen = l
		sc.longestPath = fullPath
	}
	l = int64(len(f.name))
	if l > sc.maxFNameLen {
		sc.maxFNameLen = l
		sc.longestFName = f.name
	}
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
		//fmt.Printf("  Named pipe: [%s]\n", f.fullpath)
		f.isSpecial = true

	case mode&os.ModeCharDevice != 0:
		//fmt.Printf("  Character Device: [%s]\n", f.fullpath)
		sc.nCharDevices++
		f.isSpecial = true

	case mode&os.ModeDevice != 0:
		//fmt.Printf("  Block device: [%s]\n", f.fullpath)
		sc.nBlockDevices++
		f.isSpecial = true

	case mode&os.ModeSocket != 0:
		//fmt.Printf("  Socket: [%s]\n", f.fullpath)
		sc.nSockets++
		f.isSpecial = true

	default:
		fmt.Printf("  Unknown file type (%v): [%s]\n", mode, f.fullpath)
	}
	err = sysStat(sc, &f)
	if err != nil {
		//fmt.Println(err)
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
	if sc.nErrors > 0 {
		fmt.Printf(", Error: %d", sc.nErrors)
	}
	if sc.nBlockDevices > 0 {
		fmt.Printf(", Block device: %d", sc.nBlockDevices)
	}
	if sc.nCharDevices > 0 {
		fmt.Printf(", Character device: %d", sc.nCharDevices)
	}
	fmt.Printf(", Depth: %d\n", sc.reachedDepth)
	if !sc.hideMax {
		fmt.Printf("  Deepest: %s\n", sc.deepestPath)
		fmt.Printf("  Longest path (%d): %s\n", sc.maxPathLen, sc.longestPath)
		fmt.Printf("  Longest name (%d): %s", sc.maxFNameLen, sc.longestFName)
		fmt.Println()
	}
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

func countDigits(n int64) int {
	var c int = 0
	for n != 0 {
		c++
		n /= 10
	}
	return c
}

func scan(sc *s_scan, files *[]file, path string, depth int64) (*file, error) {
	f, err := fullStat(sc, path, depth)
	if err != nil {
		// fmt.Println(err)
		return nil, err
	}

	if !f.isDir {
		ncduAdd(sc, f)
	}
	if f.isOtherFs {
		ncduAdd(sc, f)
		return f, nil
	}
	if f.isSymlink || !f.isDir {
		if files != nil {
			*files = append(*files, *f)
		}
		if len(sc.bigfiles) > sc.maxBigFiles*4 {
			sort.Sort(szDesc(sc.bigfiles))
			sc.bigfiles = sc.bigfiles[0:sc.maxBigFiles]
		}
		sc.bigfiles = append(sc.bigfiles, *f)
		return f, nil
	}

	fs, err := ioutil.ReadDir(path)
	if err != nil {
		sc.nDenied++
		f.readError = true
		// fmt.Printf("ReadDir err on \"%s\", len(fs)=%d\n", path, len(fs))
	}

	ncduOpenDir(sc)
	ncduAdd(sc, f)

	var size, du, items int64 = f.size, f.diskUsage, 0
	var ptr *[]file
	l := len(fs)
	if l > 0 {
		ncduNext(sc)
	}
	for n, i := range fs { // Calculate total size by recursive scanning
		ptr = files
		if depth > 1 {
			ptr = nil // Forget details for deep directories
		}
		items++
		var subpath string
		if path == "." {
			subpath = i.Name()
		} else {
			subpath = path + sc.pathSeparator + i.Name()
		}
		cf, err := scan(sc, ptr, subpath, depth+1)
		if err != nil {
			//fmt.Println(err)
			continue
		}
		if n < l-1 {
			ncduNext(sc)
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
	ncduCloseDir(sc)
	return &fo, nil
}

func showmax(sc *s_scan, total *file) {
	if total.diskUsage == 0 {
		return
	}
	if sc.maxBigFiles <= 0 {
		return
	}
	sort.Sort(szDesc(sc.bigfiles)) // sort biggest files by descending size
	fmt.Println()
	fmt.Println("  --------- BIGGEST FILES -------------")
	var i int = 0
	var sum, rsum int64 = 0, 0
	fi := sc.bigfiles
	for _, f := range fi {
		i++
		if i > sc.maxBigFiles {
			rsum += f.diskUsage
			continue
		}
		f.path = smartTruncate(f.path, sc.maxNameLen+18)
		fmt.Printf("%3d.%12s| %s\n", i, fmtSz(f.diskUsage), f.path)
		sum += f.diskUsage
	}
	x := "  =%13s| %.02f%% of total disk usage\n"
	p := float64(sum*100.0) / float64(total.diskUsage)
	fmt.Printf(x, fmtSz(sum), p)
}

func show(sc *s_scan, fi []file, total *file) {
	if sc.foundBoundary {
		fmt.Println()
	}
	if total.diskUsage == 0 {
		fmt.Println("  Total disk usage is zero.")
		printFileTypes(sc)
		return
	}
	sort.Sort(szDesc(fi))     // sort files and folders by descending size
	var fmtNameLen int64 = 11 // minimum for the total line
	var rDiskUsage int64 = 0  // remaining disk usage
	var rItems int64 = 0      // remaining items
	var i int = 0
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
	nf := fmt.Sprintf("%%%ds", fmtNameLen+1)
	cf := fmt.Sprintf("%%%ds", countDigits(total.diskUsage)+1)
	mf := fmt.Sprintf("%%%dd", countDigits(sc.nItems)+1)
	var strfmt = "%3d." + nf + "|" + cf + "|%6.2f%%"
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
			fmt.Printf("|"+mf+" items", f.items)
		}
		fmt.Println()
	}
	strfmt = "    " + nf + "|" + cf + "|" // spaces for line number width
	if rDiskUsage > 0 {
		p := float64(rDiskUsage*100.0) / float64(total.diskUsage)
		s := strfmt + "%6.2f%%|" + mf + " items\n"
		fmt.Printf(s, "REMAINING", fmtSz(rDiskUsage), p, rItems)
	}
	strfmt += "\n"
	fmt.Printf(strfmt, "TOTAL", fmtSz(total.diskUsage))
	fmt.Printf(strfmt, "Apparent size", fmtSz(total.size))
	fmt.Println()
	printFileTypes(sc)
}

/* Change working directory if needed */
func changeDir(args []string) string {
	var dir string
	if len(args) == 0 { // return "current directory"
		dir, _ = os.Getwd()
		return dir
	}
	if len(args) > 1 {
		fmt.Printf("[Err] Can only scan one top directory: got %d\n", len(args))
		fmt.Println()
		flag.Usage()
		os.Exit(2)
	}
	err := os.Chdir(args[0])
	if err != nil {
		fmt.Printf("ERROR: Cannot change directory to %s\n%v\n", args[0], err)
		fmt.Println()
		flag.Usage()
		os.Exit(2)
	}
	dir, err = os.Getwd()
	if err != nil {
		panic(err)
	}
	return dir
}

/* Check command line arguments */
func usage(sc *s_scan) {
	flag.Usage = func() {
		fmt.Println(" Copyright (c) 2019 Joseph Paul <joseph.paul1@gmx.com>")
		fmt.Println(" https://bitbucket.org/josephpaul0/tdu")
		fmt.Println()
		fmt.Printf(" Usage: %s [options] [directory]\n", os.Args[0])
		fmt.Println()
		flag.PrintDefaults()
		fmt.Println()
	}
	mb := flag.Int("b", dft_MAXBIGFILES, "Number of big files shown")
	ml := flag.Int("l", dft_MAXSHOWNLINES, "Number of depth1 items shown")
	ex := flag.String("o", "", "Export result to Ncdu's JSON format")
	nm := flag.Bool("nomax", false, "Do not show deepest and longest paths")
	vs := flag.Bool("version", false, "Program info and usage")
	sl := flag.Bool("license", false, "Show the GNU General Public License V2")
	flag.Parse() // NArg (int)
	if *sl {
		showLicense()
		os.Exit(2)
	}
	if *vs {
		flag.Usage()
		os.Exit(2)
	}
	sc.maxShownLines = dft_MAXSHOWNLINES
	if *ml >= 0 {
		sc.maxShownLines = *ml
	}
	sc.maxBigFiles = dft_MAXBIGFILES
	if *mb >= 0 {
		sc.maxBigFiles = *mb
	}
	sc.hideMax = *nm
	if *ex != "" {
		sc.export = true
		sc.exportPath = *ex
	}
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
	fmt.Println("\n=========== Top Disk Usage v1.28 (GNU GPL) ===========\n")
	sc := newScanStruct(start)
	usage(sc)
	d := changeDir(flag.Args()) // step 1
	getConsoleWidth(sc)
	detectOS(sc)
	fmt.Printf("  OS: %s,", sc.os)
	fmt.Printf(" scanning [%s]...\n", d)
	ncduInit(sc)
	var fi []file
	t, _ := scan(sc, &fi, ".", 1) // Step 2
	show(sc, fi, t)               // Step 3
	showmax(sc, t)                // step 4
	ncduEnd(sc)
	showElapsed(sc)
}
