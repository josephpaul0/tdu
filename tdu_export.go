/* Top Disk Usage.
 * Copyright (C) 2019 Joseph Paul <joseph.paul1@gmx.com>
 * https://github.com/josephpaul0/tdu
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
	"time"
)

const (
	ncdu_INIT = iota
	ncdu_END
	ncdu_OPENDIR
	ncdu_CLOSEDIR
	ncdu_NEXT
)

func initExport(sc *s_scan) {
	mode := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	f, err := os.OpenFile(sc.exportPath, mode, 0666)
	if err != nil {
		fmt.Printf("\n  [ERROR] Cannot open export file: %v\n\n", err)
		os.Exit(1)
	}
	sc.exportFile = f
}

func ncduOpe(operation int, sc *s_scan) {
	if !sc.export {
		return
	}
	var s string
	switch operation {
	case ncdu_INIT:
		initExport(sc)
		s = "[1,1,{\"progname\":\"tdu\","
		s += fmt.Sprintf("\"progver\":\"%s\",", prg_VERSION)
		s += fmt.Sprintf("\"timestamp\":%d},\n", time.Now().Unix())
	case ncdu_OPENDIR:
		s = "["
	case ncdu_CLOSEDIR:
		s = "]"
	case ncdu_NEXT:
		s = ",\n"
	case ncdu_END:
		s = "]\n"
	default:
		panic("Unknown operation")
	}
	sc.exportFile.WriteString(s)
	if operation == ncdu_END {
		sc.exportFile.Close()
	}
}

func ncduOpenDir(sc *s_scan)  { ncduOpe(ncdu_OPENDIR, sc) }
func ncduCloseDir(sc *s_scan) { ncduOpe(ncdu_CLOSEDIR, sc) }
func ncduNext(sc *s_scan)     { ncduOpe(ncdu_NEXT, sc) }
func ncduEnd(sc *s_scan)      { ncduOpe(ncdu_END, sc) }
func ncduInit(sc *s_scan)     { ncduOpe(ncdu_INIT, sc) }

func ncduDiskUsage(sc *s_scan, f *file) (int64, bool) {
	if f.nLinks > 1 && !f.isDir { // Hardlinks exist, recalculate disk usage
		return 512 * f.nBlocks512, true
	}
	return f.diskUsage, false
}

func cleanName(s string) string {
	rs := []rune(s)
	rd := make([]rune, 0, len(s))
	for i := 0; i < len(rs); i++ {
		if rs[i] <= 31 || rs[i] == 34 || rs[i] == 127 {
			u := []rune(fmt.Sprintf("\\u00%02X", rs[i]))
			rd = append(rd, u...)
		} else {
			rd = append(rd, rs[i])
		}
	}
	return string(rd)
}

func ncduAdd(sc *s_scan, f *file) {
	if !sc.export {
		return
	}
	name := cleanName(f.name)
	if f.depth == 1 {
		name, _ = os.Getwd()
	}
	s := fmt.Sprintf("{\"name\":\"%s\"", name)
	if f.size > 0 && !f.isOtherFs {
		s += fmt.Sprintf(",\"asize\":%d", f.size)
	}
	du, hl := ncduDiskUsage(sc, f)
	if du > 0 && !f.isOtherFs {
		s += fmt.Sprintf(",\"dsize\":%d", du)
	}
	if f.depth == 1 || f.isOtherFs {
		s += fmt.Sprintf(",\"dev\":%d", f.deviceId)
	}
	s += fmt.Sprintf(",\"ino\":%d", f.inode)
	if hl {
		s += ",\"hlnkc\":true"
	}
	if !f.isDir && !f.isRegular {
		s += ",\"notreg\":true"
	}
	if f.readError {
		s += ",\"read_error\":true"
	}
	if f.isOtherFs {
		s += ",\"excluded\":\"othfs\""
	}
	s += "}"
	sc.exportFile.WriteString(s)
}
