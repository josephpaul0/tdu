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
	"log"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// Code inspired by termbox-go (https://github.com/nsf/termbox-go)
type (
	coord struct {
		x int16
		y int16
	}
	rect struct {
		left   int16
		top    int16
		right  int16
		bottom int16
	}
	scrbuf struct {
		size         coord
		cursor_pos   coord
		attr         uint16
		wnd          rect
		max_win_size coord
	}
)

func (this coord) uintptr() uintptr {
	return uintptr(*(*int32)(unsafe.Pointer(&this)))
}

func (this *rect) uintptr() uintptr {
	return uintptr(unsafe.Pointer(this))
}

func (this *scrbuf) uintptr() uintptr {
	return uintptr(unsafe.Pointer(this))
}

var (
	hnd              uintptr // console output
	fromCmdLine      bool
	ttyWidth         int = 72
	kernel               = syscall.NewLazyDLL("kernel32.dll")
	setMode              = kernel.NewProc("SetConsoleMode")
	getMode              = kernel.NewProc("GetConsoleMode")
	setScrBufferSize     = kernel.NewProc("SetConsoleScreenBufferSize")
	setWindowInfo        = kernel.NewProc("SetConsoleWindowInfo")
	getScrBufferInfo     = kernel.NewProc("GetConsoleScreenBufferInfo")
	setCursorPos         = kernel.NewProc("SetConsoleCursorPosition")
	fillOutputChar       = kernel.NewProc("FillConsoleOutputCharacterW")
	fillOutputAttr       = kernel.NewProc("FillConsoleOutputAttribute")
	setConsoleTitle      = kernel.NewProc("SetConsoleTitleA")
)

func osInit() bool {
	env := os.Environ()
	fromCmdLine = false
	for _, v := range env {
		if strings.HasPrefix(v, "PROMPT") {
			fromCmdLine = true
			break
		}
	}
	hnd = uintptr(syscall.Handle(os.Stdout.Fd()))

	if !fromCmdLine {
		m := fmt.Sprintf("Top Disk Usage v%s (GNU GPL)", prg_VERSION)
		setTitle(m)
		fmt.Println()
		fmt.Println("  This program should be run from the command line.")
		pressAnyKey("  Press any key to continue...")
	}
	return true
}

func osEnd() bool {
	if !fromCmdLine {
		pressAnyKey("  Press any key to exit...")
	}
	return true
}

func getTtyWidth() int {
	return ttyWidth
}

func clearTty() {
	sz := updateScreenSize()
	eraseScreen(int(sz.x * sz.y))
	moveCursor(0, 0)
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

func pressAnyKey(msg string) {
	var m uint32
	h := uintptr(syscall.Handle(os.Stdin.Fd()))
	getConsoleMode(h, &m)
	setInputMode(h, 0)
	defer setInputMode(h, m)
	fmt.Println()
	fmt.Printf(msg)
	b := make([]byte, 10)
	if _, err := os.Stdin.Read(b); err != nil {
		log.Fatal(err)
	}
	fmt.Println()
	fmt.Println()
}

func getScreenSize() coord {
	var s scrbuf
	getScreenBuffer(&s)
	sz := coord{s.wnd.right - s.wnd.left + 1, s.wnd.bottom - s.wnd.top + 1}
	return sz
}

func updateScreenSize() coord {
	size := getScreenSize()
	if size.x < 100 {
		size.x = 100
	}
	if size.y < 43 {
		size.y = 43
	}
	win := rect{0, 0, size.x - 1, size.y - 1}
	ttyWidth = int(size.x) - 1
	size.y += 100 // enable scrollback
	setScreenBuffer(size)
	setWindow(&win)
	return size
}

// WIN32 native functions

func setTitle(m string) {
	b := append([]byte(m), 0)
	r, _, err := setConsoleTitle.Call(uintptr(unsafe.Pointer(&b[0])))
	if r == 0 {
		panic(err)
	}
}

func setInputMode(h uintptr, m uint32) {
	r, _, err := setMode.Call(h, uintptr(m))
	if r == 0 {
		panic(err)
	}
}

func getConsoleMode(h uintptr, m *uint32) {
	r, _, err := getMode.Call(h, uintptr(unsafe.Pointer(m)))
	if r == 0 {
		panic(err)
	}
}

func getScreenBuffer(info *scrbuf) {
	r, _, err := getScrBufferInfo.Call(hnd, info.uintptr())
	if r == 0 {
		panic(err)
	}
}

func setScreenBuffer(size coord) {
	r, _, err := setScrBufferSize.Call(hnd, size.uintptr())
	if r == 0 {
		panic(err)
	}
}

func setWindow(w *rect) {
	var absolute uint32 = 1
	r, _, err := setWindowInfo.Call(hnd, uintptr(absolute), w.uintptr())
	if r == 0 {
		panic(err)
	}
}

func eraseScreen(n int) {
	var arg uint32
	parg := uintptr(unsafe.Pointer(&arg))
	pn := uintptr(n)
	var attr uint16 = 7
	var char uint16 = 32 // space char 0x20
	var xy coord = coord{0, 0}
	pxy := xy.uintptr()
	r, _, err := fillOutputAttr.Call(hnd, uintptr(attr), pn, pxy, parg)
	if r == 0 {
		panic(err)
	}
	r, _, err = fillOutputChar.Call(hnd, uintptr(char), pn, pxy, parg)
	if r == 0 {
		panic(err)
	}
}

func moveCursor(x, y int16) {
	pos := coord{x, y}
	r, _, err := setCursorPos.Call(hnd, pos.uintptr())
	if r == 0 {
		panic(err)
	}
}
