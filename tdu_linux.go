// +build linux

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
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

var mntFlag = map[int64]string{
	0x0001: "RDONLY",      /* mount read-only */
	0x0002: "NOSUID",      /* ignore suid and sgid bits */
	0x0004: "NODEV",       /* disallow access to device special files */
	0x0008: "NOEXEC",      /* disallow program execution */
	0x0010: "SYNCHRONOUS", /* writes are synced at once */
	//0x0020: "ST_VALID",       /* f_flags support is implemented */
	0x0040: "MANDLOCK",   /* allow mandatory locks on an FS */
	0x0400: "NOATIME",    /* do not update access times */
	0x0800: "NODIRATIME", /* do not update directory access times */
	0x1000: "RELATIME",   /* update atime relative to mtime/ctime */
}

func readFlags(f int64) string {
	s := ""
	i := 0
	for k, v := range mntFlag {
		if (f & k) != 0 {
			if i > 0 {
				s += "|"
			}
			s += v
			i++
		}
	}
	return s
}

/* From LINUX_MAGIC_H + statfs + coreutils */
var fsType = map[int64]string{
	0x0000002f: "qnx4",
	0x00000187: "autofs",
	0x00001373: "devfs",
	0x0000137d: "ext",
	0x0000137f: "minix",
	0x0000138f: "minix",
	0x00001cd1: "devpts",
	0x00002468: "minix2",
	0x00002478: "minix2",
	0x00003434: "nilfs",
	0x00004244: "hfs",
	0x0000482B: "hfs+",
	0x00004858: "hfsx",
	0x00004d44: "msdos",
	0x00004d5a: "minix3",
	0x0000517b: "smb",
	0x0000564c: "ncp",
	0x00005df5: "exofs",
	0x00006969: "nfs",
	0x00007275: "romfs",
	0x000072b6: "jffs2",
	0x00009660: "isofs",
	0x00009fa0: "proc",
	0x00009fa1: "openprom",
	0x00009fa2: "usbdevice",
	0x0000adf5: "adfs",
	0x0000adff: "affs",
	0x0000ef51: "ext2_old",
	0x0000ef53: "ext2/3/4",
	0x0000f15f: "ecryptfs",
	0x00011954: "ufs",
	0x0027e0eb: "cgroup",
	0x00414a53: "efs",
	0x00c0ffee: "hostfs",
	0x00c36400: "ceph",
	0x01021994: "tmpfs",
	0x01021997: "v9fs",
	0x01161970: "gfs/gfs2",
	0x012fd16d: "_xiafs",
	0x012ff7b4: "xenix",
	0x012ff7b5: "sysv4",
	0x012ff7b6: "sysv2",
	0x012ff7b7: "coh",
	0x07655821: "rdtgroup",
	0x09041934: "anon-inode",
	0x0bad1dea: "futexfs",
	0x0bd00bd0: "lustre",
	0x11307854: "mtd_inode_fs",
	0x13661366: "balloon_kvm",
	0x15013346: "udf",
	0x19800202: "mqueue",
	0x19830326: "fhgfs",
	0x1badface: "bfs",
	0x24051905: "ubifs",
	0x28cd3d45: "cramfs",
	0x2bad1dea: "inotifyfs",
	0x2fc12fc1: "zfs",
	0x3153464a: "jfs",
	0x42465331: "befs",
	0x42494e4d: "binfmtfs",
	0x43415d53: "smack",
	0x453dcd28: "cramfs-wend",
	0x45584653: "exfs",
	0x47504653: "gpfs",
	0x50495045: "pipefs",
	0x52654973: "reiserfs",
	0x5346314d: "m1fs",
	0x5346414f: "afs",
	0x53464846: "wslfs",
	0x5346544e: "ntfs",
	0x534f434b: "sockfs",
	0x565a4653: "vzfs",
	0x57ac6e9d: "stack_end",
	0x58295829: "zsmalloc",
	0x58465342: "xfs",
	0x5a3c69f0: "aafs",
	0x61636673: "acfs",
	0x6165676c: "pstorefs",
	0x61756673: "aufs",
	0x62646576: "bdevfs",
	0x62656572: "sysfs",
	0x63677270: "cgroup2",
	0x64626720: "debugfs",
	0x64646178: "daxfs",
	0x65735543: "fusectl",
	0x65735546: "fuse",
	0x67596969: "rpc_pipefs",
	0x68191122: "qnx6",
	0x6b414653: "k-afs",
	0x6e736673: "nsfs",
	0x73636673: "securityfs",
	0x73717368: "squashfs",
	0x73727279: "btrfs_test",
	0x73757245: "coda",
	0x7461636f: "ocfs2",
	0x74726163: "tracefs",
	0x794c7630: "overlayfs",
	0x7c7c6673: "prl_fs",
	0x858458f6: "ramfs",
	0x9123683e: "btrfs",
	0x958458f6: "hugetlbfs",
	0xa501fcf5: "vxfs",
	0xaad7aaea: "panfs",
	0xabba1974: "xenfs",
	0xbacbacbc: "vmhgfs",
	0xc97e8168: "logfs",
	0xcafe4a11: "bpf_fs",
	0xde5e81e4: "efivarfs",
	0xf2f52010: "f2fs",
	0xf97cff8c: "selinux",
	0xf995e849: "hpfs",
	0xfe534d42: "smb2",
	0xff534d42: "cifs",
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

func scanMount(sc *s_scan) bool {
	if sc.partinfo == false {
		return false
	}
	file, err := os.Open("/proc/mounts")
	if err != nil {
		// fmt.Println(err)
		return false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		// device mountpoint fstype opt1,opt2,...,optn 0 0
		if len(fields) != 6 {
			continue // ignore lines without 6 fields (see format above)
		}
		for i := 0; i < 4; i++ {
			if fields[0] == sc.partition {
				sc.fsType = fields[2]
				sc.mountOptions = fields[3]
				return true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	return false
}

/* On Linux, try to find the partition name from the device number */
func getPartition(sc *s_scan, dev uint64) string {
	if sc.wsl {
		return fmt.Sprintf("Microsoft WSL [dev 0x%04X]", dev)
	}
	name := fmt.Sprintf("[dev 0x%04X]", dev)
	file, err := os.Open("/proc/partitions")
	if err != nil { // [Denied]
		// fmt.Println(err)
		return name
	}
	defer file.Close()
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
				name = fmt.Sprintf("(%d,%d) /dev/%s", h, l, fields[3])
				if dev == sc.currentDevice {
					sc.partition = fmt.Sprintf("/dev/%s", fields[3])
					sc.partinfo = true
				}
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	return name
}

func partInfo(sc *s_scan) {
	p := getPartition(sc, sc.currentDevice)
	fmt.Printf("  Partition: %s", p)
	if sc.wsl {
		fmt.Println()
		return
	}
	var statfs syscall.Statfs_t
	var total, avail, used uint64
	wd, _ := os.Getwd()
	syscall.Statfs(wd, &statfs)
	if scanMount(sc) {
		fmt.Printf(" %s %s\n", sc.fsType, sc.mountOptions)
	} else {
		t, ok := fsType[statfs.Type]
		if !ok {
			fmt.Printf(" Unknown FS Type 0x%04X", statfs.Type)
		} else {
			fmt.Printf(" Type:%s", t)
		}
		m := readFlags(statfs.Flags)
		fmt.Printf(" MFlags:%04X %s\n", statfs.Flags, m)
	}
	total = statfs.Files
	if total > 0 {
		avail = statfs.Ffree
		used = total - avail
		fmt.Printf("  Inodes   :%10d ", total)
		fmt.Printf("Avail:%10d ", avail)
		fmt.Printf("Used:%10d (%d%%)", used, used*100/total)
		fmt.Println()
	}
	total = statfs.Blocks * uint64(statfs.Bsize) / 1024
	if total > 0 {
		avail = statfs.Bavail * uint64(statfs.Bsize) / 1024
		used = total - avail
		fmt.Printf("  Size (kb):%10d ", total)
		fmt.Printf("Avail:%10d ", avail)
		fmt.Printf("Used:%10d (%d%%)\n", used, used*100/total)
	}
	fmt.Println()
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
		partInfo(sc)
	}
	if f.deviceId != sc.currentDevice {
		f.isOtherFs = true
		sc.foundBoundary = true
		fmt.Printf("  Not crossing FS boundary at %-15s %s\n",
			f.fullpath, getPartition(sc, f.deviceId))
	}
	_, ok = sc.inodes[f.inode]
	if ok { // Hardlink means inode used more than once in map
		if !f.isOtherFs { // Other FS may have a same inode number (root=2)
			f.diskUsage = 0
			sc.nHardlinks++
		}
	}
	// Each occurrence of inode is counted
	sc.inodes[f.inode]++
	return nil
}
