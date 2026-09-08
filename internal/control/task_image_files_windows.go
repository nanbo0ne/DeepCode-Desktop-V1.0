package control

import (
	"fmt"
	"os"
	"syscall"
)

func taskImageLink(info os.FileInfo) bool {
	attr, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || !ok || attr.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func checkTaskImageHandle(f *os.File, _ os.FileInfo) error {
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &info); err != nil {
		return err
	}
	if info.NumberOfLinks != 1 || info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("image must not be a hard link or reparse point")
	}
	return nil
}
