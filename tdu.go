/* Top Disk Usage.
 * Copyright (C) 2019-2021 Joseph Paul <joseph.paul1@gmx.com>
 * https://github.com/josephpaul0/tdu
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
	prg_VERSION       = "1.36"
	dft_MAXSHOWNLINES = 15
	dft_MAXEMPTYDIRS  = 0
	dft_MAXDENIEDDIRS = 0
	dft_MAXSTATERROR  = 0
	dft_MAXSTREAMS    = 0
	dft_MAXDEVICES    = 0
	dft_MAXBIGFILES   = 8
	cst_ENDPROGRESS   = "###"
	cst_PROGRESSBEAT  = 80 // ms
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
	nEmptyDir     int64    // number of empty directories
	nSymlinks     int64    // number of symlinks
	nHardlinks    int64    // number of hardlinks
	nSockets      int64    // number of sockets
	nPipes        int64    // number of named pipes
	nCharDevices  int64    // number of character devices
	nBlockDevices int64    // number of block devices
	reachedDepth  int64    // maximum directory depth reached
	maxPathLen    int64    // maximum directory path length
	maxFNameLen   int64    // maximum filename length
	currentDevice uint64   // device number of current partition
	refreshDelay  int64    // delay between progress bar updates
	maxWidth      int      // display width (tty columns)
	maxNameLen    int      // max filename length for depth = 1
	maxShownLines int      // number of depth 1 items to display
	maxBigFiles   int      // number of biggest files to display
	maxEmptyDirs  int      // number of empty directories to display
	maxDenied     int      // number of denied directories to display
	maxErrors     int      // number of 'lstat' errors to display
	maxStreams    int      // number of sockets and named pipes to display
	maxDevices    int      // number of character and block devices to display
	wsl           bool     // Windows Subsystem for Linux
	partinfo      bool     // found info about partition
	foundBoundary bool     // found other filesystems
	showMax       bool     // show deepest and longest paths
	export        bool     // export result to Ncdu's JSON format
	tty           bool     // stdout is on a TTY
	humanReadable bool     // print sizes in human readable format
	consoleMax    bool     // maximize size of console window (on Windows only)
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
	emptydirs     []string
	denieddirs    []string
	errors        []error
	streams       []string  // sockets and named pipes
	devices       []string  // character and block devices
	start         time.Time // time at process start
	msg           chan string
	done          chan bool
	sys           interface{} // OS functions
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
	w := getTtyWidth(sc)
	if w >= 72 {
		if w <= 120 {
			sc.maxWidth = w
		} else {
			sc.maxWidth = 120
		}
	}
	sc.maxNameLen = sc.maxWidth - 43 // formatting: stay below N columns
}

func newScanStruct(start time.Time, sys interface{}) *s_scan {
	var sc s_scan
	sc.pathSeparator = string(os.PathSeparator)
	sc.inodes = make(map[uint64]uint16, 256)
	sc.start = start
	sc.msg = make(chan string, 32)
	sc.done = make(chan bool)
	sc.refreshDelay = cst_PROGRESSBEAT
	sc.sys = sys
	return &sc
}

type szDesc []file

func (a szDesc) Len() int           { return len(a) }
func (a szDesc) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a szDesc) Less(i, j int) bool { return a[i].diskUsage > a[j].diskUsage }

func fmtSzHuman(size int64) string {
	var sz = float64(size)
	var unit string = "Kb"
	var d float64 = 1024
	units := []string{"Kb", "Mb", "Gb", "Tb", "Pb"}
	powers := []float64{2.0, 3.0, 4.0, 5.0, 6.0}
	for i, p := range powers {
		c := math.Pow(1024, p-1)
		if sz > c*2 {
			unit = units[i]
			d = c
		}
	}
	sz /= d
	if unit == "Kb" {
		return fmt.Sprintf("%d %s", int64(sz), unit)
	} else {
		return fmt.Sprintf("%.1f %s", sz, unit)
	}
}

func fmtSz(sc *s_scan, size int64) string { // Formats size
	if sc.humanReadable {
		return fmtSzHuman(size)
	}
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
		if sc.maxErrors > 0 {
			sc.errors = append(sc.errors, err)
		}
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
		sc.nPipes++
		if sc.maxStreams > 0 {
			s := fmt.Sprintf("[P] %s", f.fullpath)
			sc.streams = append(sc.streams, s)
		}
		f.isSpecial = true

	case mode&os.ModeCharDevice != 0:
		//fmt.Printf("  Character Device: [%s]\n", f.fullpath)
		sc.nCharDevices++
		if sc.maxDevices > 0 {
			s := fmt.Sprintf("[C] %s", f.fullpath)
			sc.devices = append(sc.devices, s)
		}
		f.isSpecial = true

	case mode&os.ModeDevice != 0:
		//fmt.Printf("  Block device: [%s]\n", f.fullpath)
		sc.nBlockDevices++
		if sc.maxDevices > 0 {
			s := fmt.Sprintf("[B] %s", f.fullpath)
			sc.devices = append(sc.devices, s)
		}
		f.isSpecial = true

	case mode&os.ModeSocket != 0:
		//fmt.Printf("  Socket: [%s]\n", f.fullpath)
		sc.nSockets++
		if sc.maxStreams > 0 {
			s := fmt.Sprintf("[S] %s", f.fullpath)
			sc.streams = append(sc.streams, s)
		}
		f.isSpecial = true

	default:
		m := fmt.Sprintf("  Unknown file type (%v): [%s]\n", mode, f.fullpath)
		push(sc, m)
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
	if sc.nEmptyDir > 0 {
		fmt.Printf(", Empty Dir: %d", sc.nEmptyDir)
	}
	if sc.nSymlinks > 0 {
		fmt.Printf(", Symlink: %d", sc.nSymlinks)
	}
	if sc.nHardlinks > 0 {
		fmt.Printf(",\n  Hardlink: %d", sc.nHardlinks)
	}
	if sc.nSockets > 0 {
		fmt.Printf(", Socket: %d", sc.nSockets)
	}
	if sc.nDenied > 0 {
		fmt.Printf(", ")
		msg := fmt.Sprintf("Denied: %d", sc.nDenied)
		printAlert(sc, msg)
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
	if sc.showMax {
		fmt.Printf("  Deepest: %s\n", sc.deepestPath)
		fmt.Printf("  Longest path (%d): %s\n", sc.maxPathLen, sc.longestPath)
		fmt.Printf("  Longest name (%d): %s", sc.maxFNameLen, sc.longestFName)
		fmt.Println()
	}
}

func smartTruncate(name string, max int) string { // cut in the middle
	l := len(name)
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
		if sc.maxDenied > 0 {
			sc.denieddirs = append(sc.denieddirs, f.path)
		}
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
	if l == 0 {
		sc.nEmptyDir++
		if sc.maxEmptyDirs > 0 {
			sc.emptydirs = append(sc.emptydirs, f.path)
		}
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
		fmt.Printf("%3d.%12s| %s\n", i, fmtSz(sc, f.diskUsage), f.path)
		sum += f.diskUsage
	}
	x := "  =%13s| %.02f%% of total disk usage\n"
	p := float64(sum*100.0) / float64(total.diskUsage)
	fmt.Printf(x, fmtSz(sc, sum), p)
}

func showempty(sc *s_scan) {
	if sc.maxEmptyDirs <= 0 || len(sc.emptydirs) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("  --------- EMPTY DIRECTORIES ---------")
	for i, d := range sc.emptydirs {
		i++
		if i > sc.maxEmptyDirs {
			break
		}
		fmt.Printf("%3d. %s\n", i, d)
	}
}

func showdenied(sc *s_scan) {
	if sc.maxDenied <= 0 || len(sc.denieddirs) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("  --------- ACCESS DENIED -------------")
	for i, d := range sc.denieddirs {
		i++
		if i > sc.maxDenied {
			break
		}
		fmt.Printf("%3d. %s\n", i, d)
	}
}

func showerrors(sc *s_scan) {
	if sc.maxErrors <= 0 || len(sc.errors) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("  --------- FILE STATUS ERROR ---------")
	for i, d := range sc.errors {
		i++
		if i > sc.maxErrors {
			break
		}
		fmt.Printf("%3d. %s\n", i, d)
	}
}

func showstreams(sc *s_scan) {
	if sc.maxStreams <= 0 || len(sc.streams) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("  --------- SOCKETS AND PIPES ---------")
	for i, d := range sc.streams {
		i++
		if i > sc.maxStreams {
			break
		}
		fmt.Printf("%3d. %s\n", i, d)
	}
}

func showdevices(sc *s_scan) {
	if sc.maxDevices <= 0 || len(sc.devices) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("  --------- DEVICES -------------------")
	for i, d := range sc.devices {
		i++
		if i > sc.maxDevices {
			break
		}
		fmt.Printf("%3d. %s\n", i, d)
	}
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
	sort.Sort(szDesc(fi))    // sort files and folders by descending size
	var fmtNameLen int = 11  // minimum for the total line
	var rDiskUsage int64 = 0 // remaining disk usage
	var rItems int64 = 0     // remaining items
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
		l := len(f.name)
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
	var strfmt = "%3d." + nf + "|" + cf + "|%6.2f%%|"
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
		fmt.Printf(strfmt, i, f.name, fmtSz(sc, f.diskUsage), p)
		if f.isDir {
			fmt.Printf(mf+" items", f.items)
		}
		fmt.Println()
	}
	strfmt = "    " + nf + "|" + cf + "|" // spaces for line number width
	if rDiskUsage > 0 {
		p := float64(rDiskUsage*100.0) / float64(total.diskUsage)
		s := strfmt + "%6.2f%%|" + mf + " items\n"
		fmt.Printf(s, "REMAINING", fmtSz(sc, rDiskUsage), p, rItems)
	}
	strfmt += "\n"
	fmt.Printf(strfmt, "DISK SPACE", fmtSz(sc, total.diskUsage))
	fmt.Printf(strfmt, "TOTAL SIZE", fmtSz(sc, total.size))
	fmt.Println()
	printFileTypes(sc)
}

/* Change working directory if needed */
func changeDir(args []string) (string, error) {
	var dir string
	if len(args) == 0 { // return "current directory"
		dir, _ = os.Getwd()
		return dir, nil
	}
	err := os.Chdir(args[0])
	if err != nil {
		e2 := fmt.Errorf("Cannot change directory to %s\n%v", args[0], err)
		return dir, e2
	}
	dir, err = os.Getwd()
	if err != nil {
		panic(err)
	}
	return dir, nil
}

/* Check command line arguments */
func usage(sc *s_scan) []string {
	flag.Usage = func() {
		showTitle()
		fmt.Println(" Copyright (c) 2019-2021 Joseph Paul <joseph.paul1@gmx.com>")
		fmt.Println(" https://github.com/josephpaul0/tdu")
		fmt.Println()
		fmt.Printf(" Usage: %s [options] [directory]\n", os.Args[0])
		fmt.Println()
		flag.PrintDefaults()
		fmt.Println()
		fmt.Printf(" Compiled with Go version %s", runtime.Version())
		fmt.Println()
		fmt.Println()
	}
	mb := flag.Int("b", dft_MAXBIGFILES, "Number of big files shown")
	ml := flag.Int("l", dft_MAXSHOWNLINES, "Number of depth1 items shown")
	me := flag.Int("e", dft_MAXEMPTYDIRS, "Number of empty directories shown (default 0)")
	md := flag.Int("d", dft_MAXDENIEDDIRS, "Number of access denied directories shown (default 0)")
	ms := flag.Int("s", dft_MAXSTATERROR, "Number of file status errors shown (default 0)")
	mf := flag.Int("f", dft_MAXDEVICES, "Number of devices shown (default 0)")
	mt := flag.Int("t", dft_MAXSTREAMS, "Number of sockets and named pipes shown (default 0)")
	ex := flag.String("o", "", "Export result to Ncdu's JSON format")
	nm := flag.Bool("max", false, "Show deepest and longest paths")
	vs := flag.Bool("version", false, "Program info and usage")
	sl := flag.Bool("license", false, "Show the GNU General Public License V2")
	hu := flag.Bool("human", true, "Print sizes in human readable format.\nUse --human=false to print in kilobytes instead.")
	cm := flag.Bool("consolemax", false, "Maximize console window (on Windows only)")
	flag.Parse() // NArg (int)
	if *sl {
		showLicense()
		os.Exit(2)
	}
	if *vs {
		flag.Usage()
		os.Exit(2)
	}
	args := flag.Args()
	if (len(args) > 0) && (args[0] == "/?") {
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
	sc.maxEmptyDirs = dft_MAXEMPTYDIRS
	if *me >= 0 {
		sc.maxEmptyDirs = *me
	}
	sc.maxDenied = dft_MAXDENIEDDIRS
	if *md >= 0 {
		sc.maxDenied = *md
	}
	sc.maxErrors = dft_MAXSTATERROR
	if *ms >= 0 {
		sc.maxErrors = *ms
	}
	sc.maxDevices = dft_MAXDEVICES
	if *mf >= 0 {
		sc.maxDevices = *mf
	}
	sc.maxStreams = dft_MAXSTREAMS
	if *mt >= 0 {
		sc.maxStreams = *mt
	}
	sc.showMax = *nm
	sc.humanReadable = *hu
	sc.consoleMax = *cm
	if *ex != "" {
		sc.export = true
		sc.exportPath = *ex
	}
	if len(flag.Args()) > 1 {
		fmt.Println()
		fmt.Printf("[ERROR] can only scan one top directory: got %d", len(args))
		fmt.Println()
		fmt.Println()
		fmt.Println("[TIP] Use double-quotes around the directory path if it contains spaces.")
		fmt.Println("[TIP] Example: tdu.exe \"C:\\Program Files\"")
		fmt.Println()
		flag.Usage()
		os.Exit(2)
	}
	return args
}

func showElapsed(sc *s_scan) {
	elapsed := time.Since(sc.start)
	fmt.Printf("\n  Total time: %.3f s\n\n", elapsed.Seconds())
}

func showProgress(sc *s_scan) {
	var i int
	var m string
	space := strings.Repeat(" ", 42)
	fmt.Println()
	for {
		time.Sleep(time.Duration(sc.refreshDelay) * time.Millisecond)
		select {
		case m = <-sc.msg:
			fmt.Print(space)
			fmt.Print("\r")
			if m != cst_ENDPROGRESS {
				fmt.Println(m)
			}
		default:
			i++
			printProgress(sc)
		}
		if m == cst_ENDPROGRESS {
			break
		}
	}
	// fmt.Printf("\nEmpty loops: %d\n", i)
	sc.done <- true
}

func endProgress(sc *s_scan) {
	if sc.tty {
		sc.msg <- cst_ENDPROGRESS
		<-sc.done
	}
}

func push(sc *s_scan, msg string) {
	sc.msg <- msg
}

func showTitle() {
	spc := strings.Repeat("=", 11)
	fmt.Println()
	fmt.Printf("%s Top Disk Usage v%s (GNU GPL) %s", spc, prg_VERSION, spc)
	fmt.Println()
	fmt.Println()
}

func relocate(sc *s_scan, args []string) string {
	d, err := changeDir(flag.Args())
	if err != nil {
		showTitle()
		fmt.Println(err)
		fmt.Println()
		os.Exit(2)
	}
	return d
}

func showResults(sc *s_scan, fi []file, total *file) {
	show(sc, fi, total) // Step 3
	showmax(sc, total)  // step 4
	showempty(sc)
	showdenied(sc)
	showerrors(sc)
	showstreams(sc)
	showdevices(sc)
}

func startProgress(sc *s_scan) {
	if sc.tty {
		go showProgress(sc)
	} else {
		fmt.Fprintln(os.Stderr, "  Please wait...")
	}
}

/* Basically, the process has got several steps:
 * 1. change directory to given path
 * 2. scan all files recursively, collecting 'stat' data
 * 3. sort results and output a list of biggest items at depth 1.
 * 4. show the largest files at any depth.
 */
func main() {
	_, sys := osInit()
	start := time.Now()
	sc := newScanStruct(start, sys)
	args := usage(sc)
	d := relocate(sc, args) // step 1
	detectOS(sc)
	initTty(sc)
	getConsoleWidth(sc)
	showTitle()
	fmt.Printf("  OS: %s %s,", sc.os, runtime.GOARCH)
	fmt.Printf(" scanning [%s]...\n", d)
	ncduInit(sc)
	startProgress(sc)
	var fi []file
	t, _ := scan(sc, &fi, ".", 1) // Step 2
	endProgress(sc)
	showResults(sc, fi, t)
	ncduEnd(sc)
	showElapsed(sc)
	osEnd(sys)
}
