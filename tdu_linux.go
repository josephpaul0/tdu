// +build linux

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
	"syscall"
)

func tcgets() uintptr {
	return uintptr(syscall.TCGETS)
}
