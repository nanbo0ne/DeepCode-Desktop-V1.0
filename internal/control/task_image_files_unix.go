//go:build !windows

package control

import (
	"fmt"
	"os"
	"syscall"
)

func taskImageLink(info os.FileInfo) bool {
	return info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0
}

func checkTaskImageHandle(_ *os.File, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return fmt.Errorf("image must not be a hard link")
	}
	return nil
}
