// +build windows

/* Top Disk Usage.
 * Copyright (C) 2019 Joseph Paul <joseph.paul1@gmx.com>
 * https://bitbucket.org/josephpaul0/tdu
 *
 * This program is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation; either version 2 of the License, or
 * (at your option) any later version.
 */

package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Code inspired by termbox-go (https://github.com/nsf/termbox-go)
// Code also copied from MinGW32 include files
type (
	dynProc struct {
		fx   *syscall.LazyProc
		name string
	}
	win32 struct {
		kernel      *syscall.LazyDLL
		user32      *syscall.LazyDLL
		procs       []dynProc
		hOutput     uintptr
		hInput      uintptr
		hConsole    uintptr
		hMonitor    uintptr
		isatty      bool
		fromCmdLine bool
		ttyWidth    int
		cfi         console_font
		mi          monitor
		max         coord
		zero        coord
	}
	coord struct {
		x int16
		y int16
	}
	rect struct {
		left   int32
		top    int32
		right  int32
		bottom int32
	}
	scrbuf struct {
		size    coord
		cursor  coord
		attr    uint16
		wnd     rect
		max_win coord
	}
	monitor struct {
		sizeof  uint32
		monitor rect
		area    rect
		flags   uint32
	}
	console_font struct {
		n    uint32
		size coord
	}
)

const (
	kFillConsoleOutputAttribute   = "FillConsoleOutputAttribute"
	kFillConsoleOutputCharacterW  = "FillConsoleOutputCharacterW"
	kWriteConsoleOutputCharacterA = "WriteConsoleOutputCharacterA"
	kGetConsoleMode               = "GetConsoleMode"
	kGetConsoleScreenBufferInfo   = "GetConsoleScreenBufferInfo"
	kGetConsoleWindow             = "GetConsoleWindow"
	kGetCurrentConsoleFont        = "GetCurrentConsoleFont"
	kGetFileType                  = "GetFileType"
	kGetStdHandle                 = "GetStdHandle"
	kSetConsoleCursorPosition     = "SetConsoleCursorPosition"
	kSetConsoleMode               = "SetConsoleMode"
	kSetConsoleScreenBufferSize   = "SetConsoleScreenBufferSize"
	kSetConsoleTitleA             = "SetConsoleTitleA"
	kSetConsoleWindowInfo         = "SetConsoleWindowInfo"
	uGetMonitorInfoW              = "GetMonitorInfoW"
	uGetSystemMetrics             = "GetSystemMetrics"
	uMonitorFromWindow            = "MonitorFromWindow"
	uMoveWindow                   = "MoveWindow"
	uShowWindow                   = "ShowWindow"
)

const (
	foreground_blue      = 0x0001 // Text color contains blue.
	foreground_green     = 0x0002 // Text color contains green.
	foreground_red       = 0x0004 // Text color contains red.
	foreground_intensity = 0x0008 // Text color is intensified.
	background_blue      = 0x0010 // Background color contains blue.
	background_green     = 0x0020 // Background color contains green.
	background_red       = 0x0040 // Background color contains red.
	background_intensity = 0x0080 // Background color is intensified.
)

func dyncall(addr uintptr, a []uintptr) (r1, r2 uintptr, lastErr error) {
	l := len(a)
	s3 := syscall.Syscall
	s6 := syscall.Syscall6
	switch l {
	case 0:
		return s3(addr, uintptr(l), 0, 0, 0)
	case 1:
		return s3(addr, uintptr(l), a[0], 0, 0)
	case 2:
		return s3(addr, uintptr(l), a[0], a[1], 0)
	case 3:
		return s3(addr, uintptr(l), a[0], a[1], a[2])
	case 4:
		return s6(addr, uintptr(l), a[0], a[1], a[2], a[3], 0, 0)
	case 5:
		return s6(addr, uintptr(l), a[0], a[1], a[2], a[3], a[4], 0)
	case 6:
		return s6(addr, uintptr(l), a[0], a[1], a[2], a[3], a[4], a[5])
	default:
		panic("dyncall with too many arguments")
	}
}

func createWin32() *win32 {
	var w win32
	w.zero = coord{0, 0}
	w.ttyWidth = 80
	w.fromCmdLine = false
	return &w
}

func (w *win32) find(name string) int {
	for i, p := range w.procs {
		if p.name == name {
			//fmt.Printf("[findProc] Proc '%s' at index %d\n", name, i)
			return i
		}
	}
	fmt.Printf("[findProc] Proc not found '%s'\n", name)
	return -1
}

func (w *win32) call(name string, a ...uintptr) (bool, uintptr) {
	i := w.find(name)
	p := w.procs[i].fx.Addr()
	r, _, err := dyncall(p, a)
	if r == 0 && name != uGetSystemMetrics { // ugly
		fmt.Printf("Win32 function '%s' failed", name)
		fmt.Println()
		fmt.Println(err)
		time.Sleep(6 * time.Second)
	}
	return (r != 0), r
}

func (w *win32) attach(dllName string, fxNames []string) {
	dll := syscall.NewLazyDLL(dllName)
	for _, name := range fxNames {
		p := dll.NewProc(name)
		e := p.Find()
		if e != nil {
			panic(e)
		}
		dyn := dynProc{name: name, fx: p}
		w.procs = append(w.procs, dyn)
	}
}

func (w *win32) populate() {
	kProcs := []string{
		kFillConsoleOutputAttribute,
		kFillConsoleOutputCharacterW,
		kWriteConsoleOutputCharacterA,
		kGetConsoleMode,
		kGetConsoleScreenBufferInfo,
		kGetConsoleWindow,
		kGetCurrentConsoleFont,
		kGetFileType,
		kGetStdHandle,
		kSetConsoleCursorPosition,
		kSetConsoleMode,
		kSetConsoleScreenBufferSize,
		kSetConsoleTitleA,
		kSetConsoleWindowInfo,
	}
	uProcs := []string{
		uGetMonitorInfoW,
		uGetSystemMetrics,
		uMonitorFromWindow,
		uMoveWindow,
		uShowWindow,
	}
	w.attach("kernel32.dll", kProcs)
	w.attach("user32.dll", uProcs)
	// fmt.Printf("Total : %d procs\n", len(w.procs))
}

func (w *win32) setIO() bool {
	const (
		std_input         = uint32(0xFFFFFFF6)
		std_output        = uint32(0xFFFFFFF5)
		file_type_char    = 0x0002
		file_type_disk    = 0x0001
		file_type_pipe    = 0x0003
		file_type_remote  = 0x8000
		file_type_unknown = 0x0000
	)
	b, r := w.getStdHandle(std_input)
	if !b {
		return false
	}
	w.hInput = r
	b, r = w.getStdHandle(std_output)
	if !b {
		return false
	}
	w.hOutput = r
	t := w.getFileType(w.hOutput)
	if t == file_type_pipe || t == file_type_disk {
		//fmt.Fprintf(os.Stderr, "GetFileType shows a redirected output = 0x%04X.\n", t)
		return false
	}
	var m uint32
	b, r = w.getConsoleMode(w.hOutput, &m)
	if !b {
		fmt.Fprintln(os.Stderr, "GetConsoleMode failed on STDOUT.")
		return false
	}
	b, r = w.getConsoleMode(w.hInput, &m)
	if !b {
		fmt.Fprintln(os.Stderr, "GetConsoleMode failed on STDIN.")
		return false
	}
	w.isatty = true
	return true
}

func (w *win32) getWorkingArea() {
	const (
		sm_cxscreen  = 0
		sm_cyscreen  = 1
		sm_cxvscroll = 2
		sm_cyhscroll = 3
		sm_cycaption = 4
		sm_cxborder  = 5
		sm_cyborder  = 6
		sm_cxframe   = 32
		sm_cyframe   = 33
	)
	sm := w.getSystemMetrics
	caption := sm(sm_cycaption)
	cyframe := sm(sm_cyframe)
	cyborder := sm(sm_cyborder)
	borderHeight := caption + 2*(cyframe+cyborder)
	cxframe := sm(sm_cxframe)
	cxborder := sm(sm_cxborder)
	cxvscroll := sm(sm_cxvscroll)
	borderWidth := cxvscroll + 2*(cxframe+cxborder)
	width := w.mi.area.right - w.mi.area.left + 1 - borderWidth
	height := w.mi.area.bottom - w.mi.area.top + 1 - borderHeight
	w.max.x = int16(width) / w.cfi.size.x
	w.max.y = int16(height) / w.cfi.size.y
	// fmt.Printf("Cols = %d, Rows = %d\n", max.x, max.y)
}

func (w *win32) updateConsole() bool { // Maximize Console
	b, r := w.getConsoleWindow()
	if !b {
		return false
	}
	w.hConsole = r
	b, r = w.monitorFromWindow(w.hConsole)
	if !b {
		return false
	}
	w.hMonitor = r
	b, r = w.getMonitorInfoW()
	if !b {
		return false
	}
	b, r = w.getCurrentConsoleFont()
	if !b {
		return false
	}
	w.getWorkingArea()
	if (w.max.x == 0) || (w.max.y == 0) {
		return false
	}
	var size coord = w.max
	size.y += 100
	b, r = w.setConsoleScreenBufferSize(w.hOutput, size)
	if !b {
		return false
	}
	w.maximizeWindow(w.hConsole)
	w.eraseScreen()
	w.ttyWidth = int(w.max.x)
	return true
}

func (w *win32) getStdHandle(h uint32) (bool, uintptr) {
	invalid_handle_value := uint32(0xFFFFFFFE)
	b, r := w.call(kGetStdHandle, uintptr(h))
	if uint32(r) == invalid_handle_value {
		fmt.Println("invalid_handle_value")
		return false, r
	}
	return b, r
}

func (w *win32) getSystemMetrics(m uint16) int32 {
	_, r := w.call(uGetSystemMetrics, uintptr(m))
	return int32(r)
}

func (w *win32) isRemoteSession() bool {
	r := w.getSystemMetrics(0x1000)
	return (r != 0)
}

func (w *win32) getFileType(h uintptr) int {
	_, r := w.call(kGetFileType, h)
	return int(r)
}

func (w *win32) getConsoleMode(h uintptr, m *uint32) (bool, uintptr) {
	return w.call(kGetConsoleMode, h, uintptr(unsafe.Pointer(m)))
}

func (w *win32) setConsoleMode(h uintptr, m uint32) (bool, uintptr) {
	return w.call(kSetConsoleMode, h, uintptr(m))
}

func (w *win32) getConsoleWindow() (bool, uintptr) {
	return w.call(kGetConsoleWindow)
}

func (w *win32) monitorFromWindow(h uintptr) (bool, uintptr) {
	const nearest = 2
	return w.call(uMonitorFromWindow, h, uintptr(nearest))
}

func (w *win32) getMonitorInfoW() (bool, uintptr) {
	h := w.hMonitor
	w.mi.sizeof = uint32(unsafe.Sizeof(w.mi))
	return w.call(uGetMonitorInfoW, h, uintptr(unsafe.Pointer(&w.mi)))
}

func (w *win32) getCurrentConsoleFont() (bool, uintptr) {
	f := kGetCurrentConsoleFont
	if w.hOutput == 0 {
		panic(f)
	}
	m := 0
	return w.call(f, w.hOutput, uintptr(m), uintptr(unsafe.Pointer(&w.cfi)))
}

func (w *win32) getConsoleScreenBufferInfo(info *scrbuf) (bool, uintptr) {
	f := kGetConsoleScreenBufferInfo
	if w.hOutput == 0 {
		panic(f)
	}
	return w.call(f, w.hOutput, uintptr(unsafe.Pointer(info)))
}

func (this coord) uintptr() uintptr { // Windows is expecting a 32bit integer
	return uintptr(*(*int32)(unsafe.Pointer(&this))) // Crazy, but the only way
}

func (w *win32) setConsoleScreenBufferSize(h uintptr, size coord) (bool, uintptr) {
	f := kSetConsoleScreenBufferSize
	return w.call(f, h, size.uintptr())
}

func (w *win32) maximizeWindow(h uintptr) (bool, uintptr) {
	m := 3 // Maximize window
	f := uShowWindow
	return w.call(f, h, uintptr(m))
}

func (w *win32) setConsoleCursorPosition(pos coord) {
	f := kSetConsoleCursorPosition
	if w.hOutput == 0 {
		panic(f)
	}
	b, _ := w.call(f, w.hOutput, pos.uintptr())
	if !b {
		panic(f)
	}
}

func (w *win32) setConsoleTitle(m string) (bool, uintptr) {
	f := kSetConsoleTitleA
	b := append([]byte(m), 0)
	return w.call(f, uintptr(unsafe.Pointer(&b[0])))
}

func (w *win32) eraseScreen() {
	f := "eraseScreen"
	if w.hOutput == 0 {
		panic(f)
	}
	var arg uint32
	parg := uintptr(unsafe.Pointer(&arg))
	xy := coord{0, 0}
	area := uint32(w.max.x * w.max.y)
	f = kFillConsoleOutputAttribute
	var attr uint16 = 7
	b, _ := w.call(f, w.hOutput, uintptr(attr), uintptr(area), xy.uintptr(), parg)
	if !b {
		panic(f)
	}
	f = kFillConsoleOutputCharacterW
	var char uint16 = 32 // space char 0x20
	b, _ = w.call(f, w.hOutput, uintptr(char), uintptr(area), xy.uintptr(), parg)
	if !b {
		panic(f)
	}
	w.setConsoleCursorPosition(w.zero)
}

func (w *win32) pressAnyKey(msg string) bool {
	var m uint32
	b, _ := w.getConsoleMode(w.hInput, &m)
	if !b {
		return false
	}
	b, _ = w.setConsoleMode(w.hInput, 0)
	if !b {
		return false
	}
	defer w.setConsoleMode(w.hInput, m)
	fmt.Println()
	fmt.Printf(msg)
	bin := make([]byte, 10)
	if _, err := os.Stdin.Read(bin); err != nil {
		panic(err)
	}
	fmt.Println()
	fmt.Println()
	return true
}

func osInit() (bool, interface{}) {
	w := createWin32()
	w.populate()
	return true, w
}

func osEnd(sys interface{}) bool {
	w := sys.(*win32)
	if !w.fromCmdLine {
		w.pressAnyKey("  Press any key to exit...")
	}
	return true
}

func getTtyWidth(sc *s_scan) int {
	w := sc.sys.(*win32)
	return w.ttyWidth
}

func initTty(sc *s_scan) {
	w := sc.sys.(*win32)
	sc.tty = !w.isRemoteSession()
	if !sc.tty {
		fmt.Println("  Detected Remote Session.")
		return
	}
	sc.tty = w.setIO()
	if !sc.tty {
		fmt.Fprintln(os.Stderr, "  Not in Console output mode (redirected).")
		return
	}
	m := fmt.Sprintf("Top Disk Usage v%s (GNU GPL)", prg_VERSION)
	w.setConsoleTitle(m)
	env := os.Environ()
	for _, v := range env {
		if strings.HasPrefix(v, "PROMPT") {
			w.fromCmdLine = true
			break
		}
	}
	if !w.fromCmdLine {
		fmt.Println()
		fmt.Println("  This program should be run from the command line.")
		w.pressAnyKey("  Press any key to continue...")
	}
	sc.tty = w.updateConsole()
	sc.refreshDelay *= 3
}

func (w *win32) writeConsoleOutputCharacterA(m string) (bool, uintptr) {
	var info scrbuf
	b, r := w.getConsoleScreenBufferInfo(&info)
	if !b {
		return b, r
	}
	text := append([]byte(m), 0)
	lpc := uintptr(unsafe.Pointer(&text[0]))
	l := uintptr(uint32(len(m)))
	var arg uint32
	parg := uintptr(unsafe.Pointer(&arg))
	xy := info.cursor
	f := kWriteConsoleOutputCharacterA
	if w.hOutput == 0 {
		panic(f)
	}
	return w.call(f, w.hOutput, lpc, l, xy.uintptr(), parg)
}

func (w *win32) writeColored(attr uint16, m string) {
	if w.hOutput == 0 {
		panic("color: no console output ")
	}
	var info scrbuf
	b, _ := w.getConsoleScreenBufferInfo(&info)
	if !b {
		return
	}
	var arg uint32
	parg := uintptr(unsafe.Pointer(&arg))
	xy := info.cursor
	l := uintptr(uint32(len(m)))
	f := kFillConsoleOutputAttribute
	b, _ = w.call(f, w.hOutput, uintptr(attr), l, xy.uintptr(), parg)
	if !b {
		panic(f)
	}
	b, _ = w.writeConsoleOutputCharacterA(m)
	if !b {
		panic(f)
	}
}

func (w *win32) color(attr uint16, l uint32) {
	if w.hOutput == 0 {
		panic("color: no console output ")
	}
	var info scrbuf
	b, _ := w.getConsoleScreenBufferInfo(&info)
	if !b {
		return
	}
	var arg uint32
	parg := uintptr(unsafe.Pointer(&arg))
	xy := info.cursor
	f := kFillConsoleOutputAttribute
	b, _ = w.call(f, w.hOutput, uintptr(attr), uintptr(l), xy.uintptr(), parg)
	if !b {
		panic(f)
	}
	f = kFillConsoleOutputCharacterW
	var char uint16 = 'X'
	b, _ = w.call(f, w.hOutput, uintptr(char), uintptr(l), xy.uintptr(), parg)
	if !b {
		panic(f)
	}
}

func printProgress(sc *s_scan) {
	var c uint16
	w := sc.sys.(*win32)
	if !sc.tty {
		return
	}
	n := sc.nErrors + sc.nItems
	m := fmt.Sprintf("  [.... scanning... %6d  ....]", n)
	if sc.nErrors > 0 {
		c = foreground_red | foreground_green
	} else {
		c = foreground_green
	}
	w.writeColored(c|foreground_intensity, m)
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
