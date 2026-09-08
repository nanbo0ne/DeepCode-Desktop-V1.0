//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/desktop/internal/update"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Keep the verified bytes immutable until CreateProcess has opened the image.
func lockUpdatePackage(pending *pendingDesktopUpdate) (io.Closer, error) {
	asset, ok := selectedUpdateAsset(&pending.manifest)
	if !ok {
		return nil, fmt.Errorf("update platform mismatch")
	}
	f, err := openUpdatePackage(pending.filename)
	if err != nil {
		return nil, err
	}
	sig, err := os.ReadFile(pending.filename + ".minisig")
	if err == nil {
		err = verifyPackageContents(f, asset, sig, update.VerifyReader)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func openUpdatePackage(filename string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(filename)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), filename), nil
}

// installerCommand runs the NSIS updater, forcing $INSTDIR to dir via /D= so the
// update overwrites the current install in place. NSIS requires /D= to be the
// final, unquoted token taken verbatim to the end of the line, so the raw command
// line is set directly — exec.Command would quote a path containing spaces (e.g.
// C:\Users\Jane Doe\...) and NSIS would then mis-parse the target directory.
func installerCommand(name, dir string) *exec.Cmd {
	cmd := exec.Command(name)
	if dir != "" {
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: fmt.Sprintf(`"%s" /D=%s`, name, dir)}
	}
	return cmd
}

func isInstalledDesktop() bool {
	dir := currentInstallDir()
	if dir == "" {
		return false
	}
	for _, name := range []string{"O.R.C.A for Windows", "DeepSeek-Orca"} {
		key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall\`+name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		location, _, err := key.GetStringValue("InstallLocation")
		key.Close()
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(location); err == nil {
			location = resolved
		}
		if strings.EqualFold(filepath.Clean(location), filepath.Clean(dir)) {
			_, err := os.Stat(filepath.Join(dir, "uninstall.exe"))
			return err == nil
		}
	}
	return false
}
