package control

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/proc"
)

const maxImageAttachmentBytes = 10 * 1024 * 1024
const maxFileAttachmentBytes = 25 * 1024 * 1024
const maxAttachmentCreateAttempts = 1000

var attachmentPathSeq atomic.Uint64
var attachmentNow = time.Now
var safeAttachmentExt = regexp.MustCompile(`^\.[a-z0-9]{1,12}$`)

// SaveAttachmentDataURL stores a non-image file (dropped/pasted in the desktop
// app, where the browser exposes bytes but not a real path) under
// .orca/attachments and returns its repo-relative path for @referencing.
// origName supplies only the extension; the stored name is generated.
func SaveAttachmentDataURL(origName, dataURL string) (string, error) {
	return SaveAttachmentDataURLAt(".", origName, dataURL)
}

// SaveAttachmentDataURLAt stores an attachment under workspaceRoot without
// changing the process working directory.
func SaveAttachmentDataURLAt(workspaceRoot, origName, dataURL string) (string, error) {
	const marker = ";base64,"
	i := strings.Index(dataURL, marker)
	if !strings.HasPrefix(dataURL, "data:") || i < 0 {
		return "", fmt.Errorf("unsupported pasted file")
	}
	raw, err := base64.StdEncoding.DecodeString(dataURL[i+len(marker):])
	if err != nil {
		return "", fmt.Errorf("decode pasted file: %w", err)
	}
	if len(raw) == 0 || len(raw) > maxFileAttachmentBytes {
		return "", fmt.Errorf("attachment must be between 1 byte and 25 MB")
	}
	ext := strings.ToLower(filepath.Ext(origName))
	if !safeAttachmentExt.MatchString(ext) {
		ext = ".bin"
	}
	root, err := workspaceRootPath(workspaceRoot)
	if err != nil {
		return "", err
	}
	rel, f, err := createAttachmentFileAt(root, ext)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(rel)))
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(rel)))
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func SaveImageDataURL(dataURL string) (string, error) {
	return SaveImageDataURLAt(".", dataURL)
}

// SaveImageDataURLAt stores an image under workspaceRoot without changing the
// process working directory.
func SaveImageDataURLAt(workspaceRoot, dataURL string) (string, error) {
	const prefix = "data:"
	const marker = ";base64,"
	if !strings.HasPrefix(dataURL, prefix) {
		return "", fmt.Errorf("unsupported pasted image")
	}
	i := strings.Index(dataURL, marker)
	if i <= len(prefix) {
		return "", fmt.Errorf("unsupported pasted image")
	}
	mime := strings.ToLower(dataURL[len(prefix):i])
	raw, err := base64.StdEncoding.DecodeString(dataURL[i+len(marker):])
	if err != nil {
		return "", fmt.Errorf("decode pasted image: %w", err)
	}
	return SaveImageBytesAt(workspaceRoot, mime, raw)
}

func SaveImageBytes(declaredMime string, raw []byte) (string, error) {
	return SaveImageBytesAt(".", declaredMime, raw)
}

// SaveImageBytesAt stores an image under workspaceRoot without changing the
// process working directory.
func SaveImageBytesAt(workspaceRoot, declaredMime string, raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > maxImageAttachmentBytes {
		return "", fmt.Errorf("pasted image must be between 1 byte and 10 MB")
	}
	mime := detectedImageMime(raw)
	if mime == "" {
		return "", fmt.Errorf("pasted data is not a supported image")
	}
	if declaredMime != "" && imageExt(declaredMime) == "" {
		return "", fmt.Errorf("unsupported image type: %s", declaredMime)
	}
	ext := imageExt(mime)
	root, err := workspaceRootPath(workspaceRoot)
	if err != nil {
		return "", err
	}
	rel, f, err := createAttachmentFileAt(root, ext)
	if err != nil {
		return "", err
	}
	if n, err := f.Write(raw); err != nil {
		_ = f.Close()
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(rel)))
		return "", err
	} else if n != len(raw) {
		_ = f.Close()
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(rel)))
		return "", io.ErrShortWrite
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(rel)))
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func SaveImageFile(path string) (string, error) {
	return SaveImageFileAt(path, ".")
}

// SaveImageFileAt copies an image into workspaceRoot without changing the
// process working directory.
func SaveImageFileAt(path, workspaceRoot string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("pasted image path must not be a symlink")
	}
	if info.IsDir() || info.Size() <= 0 || info.Size() > maxImageAttachmentBytes {
		return "", fmt.Errorf("pasted image must be between 1 byte and 10 MB")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", fmt.Errorf("pasted image changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxImageAttachmentBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw) > maxImageAttachmentBytes {
		return "", fmt.Errorf("pasted image must be between 1 byte and 10 MB")
	}
	if after, err := f.Stat(); err != nil {
		return "", err
	} else if !os.SameFile(opened, after) || after.Size() != opened.Size() {
		return "", fmt.Errorf("pasted image changed while reading")
	}
	return SaveImageBytesAt(workspaceRoot, "", raw)
}

// SnapshotImageFile copies a workspace image into that workspace's attachment
// directory without depending on the process cwd.
func SnapshotImageFile(path, workspaceRoot string) (string, error) {
	raw, mime, err := readImageFile(path)
	if err != nil {
		return "", err
	}
	return writeAttachmentAt(workspaceRoot, imageExt(mime), raw)
}

func readImageFile(path string) ([]byte, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || info.IsDir() || info.Size() <= 0 || info.Size() > maxImageAttachmentBytes {
		return nil, "", fmt.Errorf("image must be a regular file between 1 byte and 10 MB")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxImageAttachmentBytes+1))
	if err != nil {
		return nil, "", err
	}
	mime := detectedImageMime(raw)
	if mime == "" {
		return nil, "", fmt.Errorf("unsupported image type")
	}
	return raw, mime, nil
}

func SaveAttachmentFile(path string) (string, error) {
	return SaveAttachmentFileAt(path, ".")
}

// SaveAttachmentFileAt copies a file into workspaceRoot without changing the
// process working directory.
func SaveAttachmentFileAt(path, workspaceRoot string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("attachment path must not be a symlink")
	}
	if info.IsDir() || info.Size() <= 0 || info.Size() > maxFileAttachmentBytes {
		return "", fmt.Errorf("attachment must be between 1 byte and 25 MB")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", fmt.Errorf("attachment changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxFileAttachmentBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw) > maxFileAttachmentBytes {
		return "", fmt.Errorf("attachment must be between 1 byte and 25 MB")
	}
	if after, err := f.Stat(); err != nil {
		return "", err
	} else if !os.SameFile(opened, after) || after.Size() != opened.Size() {
		return "", fmt.Errorf("attachment changed while reading")
	}
	ext := strings.ToLower(filepath.Ext(path))
	if !safeAttachmentExt.MatchString(ext) {
		ext = ".bin"
	}
	root, err := workspaceRootPath(workspaceRoot)
	if err != nil {
		return "", err
	}
	rel, dst, err := createAttachmentFileAt(root, ext)
	if err != nil {
		return "", err
	}
	if _, err := dst.Write(raw); err != nil {
		_ = dst.Close()
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(rel)))
		return "", err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(rel)))
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func SaveClipboardImage() (string, error) {
	return SaveClipboardImageAt(".")
}

// SaveClipboardImageAt stores the native clipboard image under workspaceRoot
// without changing the process working directory.
func SaveClipboardImageAt(workspaceRoot string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return saveDarwinClipboardImage(workspaceRoot)
	case "windows":
		return saveWindowsClipboardImage(workspaceRoot)
	case "linux":
		return saveLinuxClipboardImage(workspaceRoot)
	default:
		return "", fmt.Errorf("clipboard image paste is not supported on %s yet", runtime.GOOS)
	}
}

func saveWindowsClipboardImage(workspaceRoot string) (string, error) {
	// Windows PowerShell 5.1 (preinstalled) reaches the GUI clipboard; pwsh (Core)
	// lacks Get-Clipboard -Format Image, so invoke powershell.exe. The PNG is
	// returned as base64 on stdout so no temp file is involved.
	script := `Add-Type -AssemblyName System.Drawing
$img = Get-Clipboard -Format Image
if ($null -eq $img) { [Console]::Error.WriteLine('clipboard has no image'); exit 1 }
$ms = New-Object System.IO.MemoryStream
$img.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
[Convert]::ToBase64String($ms.ToArray())`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	proc.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("read clipboard image: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("read clipboard image: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return "", fmt.Errorf("decode clipboard image: %w", err)
	}
	return SaveImageBytesAt(workspaceRoot, "", raw)
}

func saveLinuxClipboardImage(workspaceRoot string) (string, error) {
	// Wayland (wl-paste) then X11 (xclip); both write image bytes to stdout.
	for _, c := range [][]string{
		{"wl-paste", "--type", "image/png", "--no-newline"},
		{"xclip", "-selection", "clipboard", "-t", "image/png", "-o"},
	} {
		if out, err := exec.Command(c[0], c[1:]...).Output(); err == nil && len(out) > 0 {
			return SaveImageBytesAt(workspaceRoot, "", out)
		}
	}
	return "", fmt.Errorf("clipboard image paste needs wl-paste (Wayland) or xclip (X11)")
}

func ImageDataURL(path string) (string, error) {
	return ImageDataURLAt(path, ".")
}

// ImageDataURLAt reads an image attachment relative to workspaceRoot without
// changing the process working directory.
func ImageDataURLAt(path, workspaceRoot string) (string, error) {
	clean, err := cleanAttachmentPathAt(path, workspaceRoot)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("attachment path must not be a symlink")
	}
	if info.IsDir() || info.Size() <= 0 || info.Size() > maxImageAttachmentBytes {
		return "", fmt.Errorf("attachment image must be between 1 byte and 10 MB")
	}
	f, err := os.Open(clean)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", fmt.Errorf("attachment changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxImageAttachmentBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw) > maxImageAttachmentBytes {
		return "", fmt.Errorf("attachment image must be between 1 byte and 10 MB")
	}
	if after, err := f.Stat(); err != nil {
		return "", err
	} else if !os.SameFile(opened, after) || after.Size() != opened.Size() {
		return "", fmt.Errorf("attachment changed while reading")
	}
	mime := detectedImageMime(raw)
	if mime == "" {
		return "", fmt.Errorf("attachment is not an image")
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}

func cleanAttachmentPath(path string) (string, error) {
	return cleanAttachmentPathAt(path, ".")
}

func cleanAttachmentPathAt(path, workspaceRoot string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("attachment path must be relative")
	}
	rootDir, err := workspaceRootPath(workspaceRoot)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	root := filepath.Join(".orca", "attachments")
	legacyRoot := filepath.Join(".deepseek-orca", "attachments")
	if clean == legacyRoot || strings.HasPrefix(clean, legacyRoot+string(filepath.Separator)) {
		root = legacyRoot
	}
	if clean == "." || clean == root || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || !strings.HasPrefix(clean, root+string(filepath.Separator)) {
		return "", fmt.Errorf("attachment path is outside .orca/attachments")
	}
	if root == filepath.Join(".orca", "attachments") {
		if err := ensureAttachmentRootAt(rootDir); err != nil {
			return "", err
		}
	}
	absoluteRoot := filepath.Join(rootDir, root)
	absolutePath := filepath.Join(rootDir, clean)
	if err := rejectSymlinkComponents(absolutePath, absoluteRoot); err != nil {
		return "", err
	}
	return absolutePath, nil
}

func rejectSymlinkComponents(path, root string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return fmt.Errorf("attachment path is outside .orca/attachments")
	}
	cur := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("attachment path must not contain symlinks")
		}
	}
	return nil
}

func ensureAttachmentRoot() error {
	return ensureAttachmentRootAt(".")
}

func ensureAttachmentRootAt(workspaceRoot string) error {
	rootDir, err := workspaceRootPath(workspaceRoot)
	if err != nil {
		return err
	}
	root := filepath.Join(rootDir, ".orca", "attachments")
	if info, err := os.Lstat(root); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("attachment directory must not be a symlink")
		}
		if !info.IsDir() {
			return fmt.Errorf("attachment path exists but is not a directory")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("attachment directory is invalid")
	}
	return nil
}

func saveDarwinClipboardImage(workspaceRoot string) (string, error) {
	for _, class := range []string{"PNGf", "JPEG"} {
		if rel, err := saveDarwinClipboardClass(class, workspaceRoot); err == nil {
			return rel, nil
		}
	}
	return "", fmt.Errorf("clipboard does not contain a supported image")
}

func saveDarwinClipboardClass(class, workspaceRoot string) (string, error) {
	root, err := workspaceRootPath(workspaceRoot)
	if err != nil {
		return "", err
	}
	rel, f, err := createAttachmentFileAt(root, ".bin")
	if err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(filepath.Join(root, filepath.FromSlash(rel)))
		return "", err
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	script := fmt.Sprintf(`
set outPath to POSIX file %q
try
	set img to the clipboard as «class %s»
on error
	error "clipboard does not contain this image type"
end try
set f to open for access outPath with write permission
try
	set eof f to 0
	write img to f
	close access f
on error errMsg
	try
		close access f
	end try
	error errMsg
end try
	`, abs, class)
	if out, err := exec.Command("osascript", "-e", script).CombinedOutput(); err != nil {
		_ = os.Remove(abs)
		return "", fmt.Errorf("read clipboard image: %s", strings.TrimSpace(string(out)))
	}
	raw, err := os.ReadFile(abs)
	_ = os.Remove(abs)
	if err != nil {
		return "", err
	}
	return SaveImageBytesAt(root, "", raw)
}

func createAttachmentFileAt(workspaceRoot, ext string) (string, *os.File, error) {
	root, err := workspaceRootPath(workspaceRoot)
	if err != nil {
		return "", nil, err
	}
	if err := ensureAttachmentRootAt(root); err != nil {
		return "", nil, err
	}
	for range maxAttachmentCreateAttempts {
		rel := attachmentPath(ext)
		abs := filepath.Join(root, filepath.FromSlash(rel))
		f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		return rel, f, nil
	}
	return "", nil, fmt.Errorf("create unique attachment path")
}

func writeAttachmentAt(workspaceRoot, ext string, raw []byte) (string, error) {
	root, err := workspaceRootPath(workspaceRoot)
	if err != nil {
		return "", err
	}
	rel, f, err := createAttachmentFileAt(root, ext)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		_ = os.Remove(abs)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(abs)
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func workspaceRootPath(workspaceRoot string) (string, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(filepath.FromSlash(root))
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	return filepath.Clean(abs), nil
}

func createAttachmentFile(ext string) (string, *os.File, error) {
	for range maxAttachmentCreateAttempts {
		rel := attachmentPath(ext)
		f, err := os.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		return rel, f, nil
	}
	return "", nil, fmt.Errorf("create unique attachment path")
}

func attachmentPath(ext string) string {
	seq := attachmentPathSeq.Add(1)
	name := fmt.Sprintf("clipboard-%s-%06d%s", attachmentNow().Format("20060102-150405.000000"), seq, ext)
	return filepath.Join(".orca", "attachments", name)
}

func detectedImageMime(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	mime := http.DetectContentType(raw[:min(len(raw), 512)])
	if imageExt(mime) == "" {
		return ""
	}
	return mime
}

func imageExt(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	return ""
}
