//go:build !unix

package utils

import "io/fs"

func linkCount(fs.FileInfo) uint64 { return 1 }
