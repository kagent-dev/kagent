//go:build unix

package utils

import (
	"io/fs"
	"syscall"
)

func linkCount(info fs.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(stat.Nlink)
	}
	return 1
}
